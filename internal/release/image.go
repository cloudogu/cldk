package release

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// ReleasedImage is an image that has been released.
type ReleasedImage struct {
	// Source is the source reference of the image as given in Options.Images, without an explicit target.
	Source string
	// Reference is the full reference of the released image, e.g.
	// "registry.cloudogu.com/official/dogu/v3/images/nexus:3.96.4-3".
	Reference string
	// Digest is the digest of the released manifest or index, which equals the digest of the source.
	Digest string
	// Status is the state of the image in the target.
	Status Status
}

// The images pass through the release in phases, each a value of its own: parsed (imageSpec), resolved in the source
// registry (resolvedImage) and checked against the target (checkedImage).

// imageSpec is an image to release as given in Options.Images, with its target computed.
type imageSpec struct {
	source name.Tag
	// targetName is the name of the image in the target, by default the last path segment of the source repository.
	targetName string
	target     name.Tag
}

// resolvedImage is an image whose manifest or index has been resolved in its source registry.
type resolvedImage struct {
	imageSpec
	desc *remote.Descriptor
}

// checkedImage is a resolved image whose target has been checked.
type checkedImage struct {
	resolvedImage
	status Status
	// existingDigest is the digest the target already has, empty if the target does not exist.
	existingDigest string
}

// parseImages parses the images as given in Options.Images and computes their targets without accessing a registry.
func parseImages(specs []string, target Target) ([]imageSpec, error) {
	images := make([]imageSpec, 0, len(specs))
	sourceByTargetName := map[string]string{}

	for _, spec := range specs {
		img, err := parseImage(spec, target)
		if err != nil {
			return nil, err
		}

		if other, ok := sourceByTargetName[img.targetName]; ok {
			return nil, fmt.Errorf("images %s and %s would both be released as %s", other, img.source, img.targetName)
		}

		sourceByTargetName[img.targetName] = img.source.String()
		images = append(images, img)
	}

	return images, nil
}

// parseImage parses an image given as "<source>" or "<source>=<target name>:<tag>".
func parseImage(spec string, target Target) (imageSpec, error) {
	source, explicitTarget, hasExplicitTarget := strings.Cut(spec, "=")

	sourceRef, err := name.ParseReference(source)
	if err != nil {
		return imageSpec{}, fmt.Errorf("invalid image reference %s: %w", source, err)
	}

	sourceTag, ok := sourceRef.(name.Tag)
	if !ok {
		return imageSpec{}, fmt.Errorf("image %s has no tag", source)
	}

	// Without a registry, the image would silently be pulled from docker.io, which hardly ever is the intended source.
	if !hasRegistry(source) {
		return imageSpec{}, fmt.Errorf("image %s has no registry, give it explicitly, e.g. docker.io/%s:%s",
			source, sourceTag.RepositoryStr(), sourceTag.TagStr())
	}

	targetName := path.Base(sourceTag.RepositoryStr())
	tag := sourceTag.TagStr()

	if hasExplicitTarget {
		targetName, tag, ok = strings.Cut(explicitTarget, ":")
		if !ok || targetName == "" || tag == "" || strings.Contains(targetName, "/") {
			return imageSpec{}, fmt.Errorf("invalid image %s: the target must be given as <name>:<tag>, e.g. %s=nexus:3.96.4-3", spec, source)
		}
	}

	targetTag, err := name.NewTag(target.imageRepository(targetName)+":"+tag, target.nameOptions()...)
	if err != nil {
		return imageSpec{}, fmt.Errorf("invalid target %s:%s for image %s: %w", targetName, tag, source, err)
	}

	return imageSpec{source: sourceTag, targetName: targetName, target: targetTag}, nil
}

// hasRegistry reports whether the image reference starts with a registry. Like docker, it takes the first path segment
// as registry if it contains a "." or ":" or is "localhost".
func hasRegistry(ref string) bool {
	first, _, hasPath := strings.Cut(ref, "/")

	return hasPath && (strings.ContainsAny(first, ".:") || first == "localhost")
}

// parseImageKeys parses the image keys as given in Options.ImageKeys and returns the keys by target name.
// Each image key must refer to the target name of one of the images.
func parseImageKeys(specs []string, images []imageSpec) (map[string]string, error) {
	targetNames := make([]string, 0, len(images))

	for _, img := range images {
		targetNames = append(targetNames, img.targetName)
	}

	slices.Sort(targetNames)

	if len(specs) > 0 && len(images) == 0 {
		return nil, errors.New("image keys require images to release (--image-key needs -i)")
	}

	keys := make(map[string]string, len(specs))

	for _, spec := range specs {
		targetName, key, ok := strings.Cut(spec, "=")
		if !ok || targetName == "" || key == "" {
			return nil, fmt.Errorf("invalid image key %q: must be given as <name>=<key>, e.g. nexus=nexus", spec)
		}

		if other, ok := keys[targetName]; ok {
			return nil, fmt.Errorf("several image keys for image %s: %s, %s", targetName, other, key)
		}

		if _, found := slices.BinarySearch(targetNames, targetName); !found {
			return nil, fmt.Errorf("image key %s refers to no released image (released images: %s)",
				spec, strings.Join(targetNames, ", "))
		}

		keys[targetName] = key
	}

	return keys, nil
}

// resolveImages resolves all images in the registries of their sources.
// The sources are always accessed with the stored docker credentials and the default transport.
func resolveImages(ctx context.Context, images []imageSpec) ([]resolvedImage, error) {
	resolved := make([]resolvedImage, 0, len(images))

	for _, img := range images {
		slog.Debug("resolving image", "source", img.source.String())

		desc, err := remote.Get(img.source,
			remote.WithContext(ctx),
			remote.WithAuthFromKeychain(authn.DefaultKeychain),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve image %s: %w", img.source, err)
		}

		resolved = append(resolved, resolvedImage{imageSpec: img, desc: desc})
	}

	return resolved, nil
}

// checkImageTargets checks the targets of all images and returns the images with their status.
func checkImageTargets(ctx context.Context, images []resolvedImage, target Target, force bool) ([]checkedImage, error) {
	checked := make([]checkedImage, 0, len(images))

	for _, img := range images {
		c, err := img.checkTarget(ctx, target, force)
		if err != nil {
			return nil, err
		}

		checked = append(checked, c)
	}

	return checked, nil
}

// checkTarget checks whether the target tag of the image already exists and returns the image with its status.
// An existing tag with another digest is a conflict unless force allows to overwrite it.
func (img resolvedImage) checkTarget(ctx context.Context, target Target, force bool) (checkedImage, error) {
	slog.Debug("checking image target", "target", img.target.String())

	existing, err := remote.Head(img.target, targetOptions(ctx, target)...)
	if isNotFound(err) {
		return checkedImage{resolvedImage: img, status: StatusPushed}, nil
	}

	if err != nil {
		return checkedImage{}, fmt.Errorf("failed to check image %s: %w", img.target, err)
	}

	checked := checkedImage{resolvedImage: img, existingDigest: existing.Digest.String()}

	switch {
	case existing.Digest == img.desc.Digest:
		checked.status = StatusSkipped
	case !force:
		checked.status = StatusConflict
	default:
		slog.Debug("overwriting image", "target", img.target.String(), "digest", existing.Digest.String())
		checked.status = StatusOverwritten
	}

	return checked, nil
}

// planned describes the image as it would be released to its target.
func (img checkedImage) planned() PlannedImage {
	return PlannedImage{
		Source:         img.source.String(),
		Reference:      img.target.String(),
		Digest:         img.desc.Digest.String(),
		ExistingDigest: img.existingDigest,
		Status:         img.status,
	}
}

// released describes the image as released to its target.
func (img checkedImage) released() ReleasedImage {
	return ReleasedImage{
		Source:    img.source.String(),
		Reference: img.target.String(),
		Digest:    img.desc.Digest.String(),
		Status:    img.status,
	}
}

// push copies the manifest or index of the image unchanged to its target, unless the target already has it.
func (img checkedImage) push(ctx context.Context, target Target) (ReleasedImage, error) {
	released := img.released()

	if released.Status == StatusSkipped {
		slog.Debug("skipping already released image", "source", img.source.String(), "target", img.target.String())

		return released, nil
	}

	slog.Debug("pushing image", "source", img.source.String(), "target", img.target.String())

	if err := remote.Push(img.target, img.desc, targetOptions(ctx, target)...); err != nil {
		return ReleasedImage{}, fmt.Errorf("failed to push image %s to %s: %w", img.source, img.target, err)
	}

	return released, nil
}

// targetOptions returns the options to access the registry of the target.
func targetOptions(ctx context.Context, target Target) []remote.Option {
	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithTransport(newTransport(target)),
	}

	if target.User != "" {
		return append(opts, remote.WithAuth(&authn.Basic{Username: target.User, Password: target.Password}))
	}

	return append(opts, remote.WithAuthFromKeychain(authn.DefaultKeychain))
}

// isNotFound reports whether err is the response of a registry to a missing manifest.
func isNotFound(err error) bool {
	var transportErr *transport.Error

	return errors.As(err, &transportErr) && transportErr.StatusCode == http.StatusNotFound
}
