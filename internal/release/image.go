package release

import (
	"context"
	"fmt"
	"log/slog"
	"path"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// ReleasedImage is an image that has been released.
type ReleasedImage struct {
	// Source is the reference of the image as given in Options.Images.
	Source string
	// Reference is the full reference of the released image, e.g.
	// "registry.cloudogu.com/official/dogu/v3/images/nexus:3.96.4-3".
	Reference string
	// Digest is the digest of the released manifest or index, which equals the digest of the source.
	Digest string
}

// image is an image to release, resolved from its source.
type image struct {
	source string
	desc   *remote.Descriptor
	// targetName is the name of the image in the target, the last path segment of the source repository.
	targetName string
	target     name.Tag
}

// resolveImages resolves all sources in the registries they refer to and computes their targets.
// The sources are always accessed with the stored docker credentials and the default transport. The credentials and
// the transport settings of the target are not used.
func resolveImages(ctx context.Context, sources []string, target Target) ([]*image, error) {
	images := make([]*image, 0, len(sources))
	sourceByTargetName := map[string]string{}

	for _, source := range sources {
		img, err := resolveImage(ctx, source, target)
		if err != nil {
			return nil, err
		}

		if other, ok := sourceByTargetName[img.targetName]; ok {
			return nil, fmt.Errorf("images %s and %s would both be released as %s", other, source, img.targetName)
		}

		sourceByTargetName[img.targetName] = source
		images = append(images, img)
	}

	return images, nil
}

func resolveImage(ctx context.Context, source string, target Target) (*image, error) {
	sourceRef, err := name.ParseReference(source)
	if err != nil {
		return nil, fmt.Errorf("invalid image reference %s: %w", source, err)
	}

	tag, ok := sourceRef.(name.Tag)
	if !ok {
		return nil, fmt.Errorf("image %s has no tag", source)
	}

	slog.Debug("resolving image", "source", source)

	desc, err := remote.Get(sourceRef,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve image %s: %w", source, err)
	}

	targetName := path.Base(tag.RepositoryStr())

	targetTag, err := name.NewTag(target.imageRepository(targetName)+":"+tag.TagStr(), target.nameOptions()...)
	if err != nil {
		return nil, fmt.Errorf("invalid target for image %s: %w", source, err)
	}

	return &image{source: source, desc: desc, targetName: targetName, target: targetTag}, nil
}

// push copies the manifest or index of the image unchanged to its target.
func (img *image) push(ctx context.Context, target Target) (ReleasedImage, error) {
	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithTransport(newTransport(target)),
	}

	if target.User != "" {
		opts = append(opts, remote.WithAuth(&authn.Basic{Username: target.User, Password: target.Password}))
	} else {
		opts = append(opts, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	}

	slog.Debug("pushing image", "source", img.source, "target", img.target.String())

	if err := remote.Push(img.target, img.desc, opts...); err != nil {
		return ReleasedImage{}, fmt.Errorf("failed to push image %s to %s: %w", img.source, img.target, err)
	}

	return ReleasedImage{Source: img.source, Reference: img.target.String(), Digest: img.desc.Digest.String()}, nil
}
