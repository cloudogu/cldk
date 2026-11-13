package release

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/google/go-containerregistry/pkg/name"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"

	"github.com/cloudogu/cldk/internal/chart"
	"github.com/cloudogu/cldk/internal/oci"
	"github.com/cloudogu/cldk/internal/project"
)

// ProjectOptions are the settings of a release from the project directory besides the target.
type ProjectOptions struct {
	// Images are the images to copy instead of building them, given as in ArchiveOptions.Images.
	// An image whose target name is an image of cldk.yaml replaces the build of that image.
	Images []string
	// ImageKeys assign the images of Images to entries of values.images, given as in ArchiveOptions.ImageKeys.
	// An image that replaces a build keeps the key of cldk.yaml without an explicit key.
	ImageKeys []string
	// Force overwrites existing release tags, image tags with another digest and an already released chart version in
	// the target. Without it, all are conflicts that abort the release before anything is built or pushed.
	Force bool
	// Dir is the project directory with cldk.yaml.
	Dir string
	// Tags are release tags of built images, each given as "<name>=<tag>", e.g. "nexus=3.96.4-1". They take
	// precedence over the tags of cldk.yaml.
	Tags []string
	// Build are the settings of the builds of the images.
	Build project.BuildOptions
}

// BuiltImage is an image of cldk.yaml that is built and pushed with its release tag.
type BuiltImage struct {
	// Name is the image name in cldk.yaml, e.g. "nexus".
	Name string
	// Reference is the full reference of the image in the target, e.g.
	// "registry.cloudogu.com/official/dogu/v3/images/nexus:3.96.4-1".
	Reference string
	// Digest is the digest of the pushed manifest or index, empty in a Plan.
	Digest string
	// Status is the state of the image in the target. A built image is never skipped, because its digest is unknown
	// before the build.
	Status Status
}

// Builder builds and pushes images, see build.Builder and project.Builder.
type Builder interface {
	project.Builder
}

// checkedBuild is an image of cldk.yaml to build whose target has been checked.
type checkedBuild struct {
	project.Image
	target name.Tag
	status Status
}

// PlanProject computes the release of the project directory to the target including all checks.
// Conflicts in the target are part of the plan, see Plan.Conflicts.
func (r *Releaser) PlanProject(ctx context.Context, target oci.Target, opts ProjectOptions) (Plan, error) {
	p, err := r.newProjectPlan(ctx, target, opts)
	if err != nil {
		return Plan{}, err
	}

	return p.summary(), nil
}

// ReleaseProject releases the project directory to the target: it builds all images of cldk.yaml, pushes them with
// their release tags, copies the images of Images and pushes the chart of the project with its version.
func (r *Releaser) ReleaseProject(ctx context.Context, target oci.Target, opts ProjectOptions) (Result, error) {
	p, err := r.newProjectPlan(ctx, target, opts)
	if err != nil {
		return Result{}, err
	}

	// All targets are checked before the first build, so that a conflict does not leave a partial release.
	if err = p.summary().Conflicts(); err != nil {
		return Result{}, err
	}

	return p.push(ctx)
}

// newProjectPlan loads the project, plans the builds, checks the builder, rewrites and lints the chart, resolves the
// copied images and checks all targets without building or pushing anything.
func (r *Releaser) newProjectPlan(ctx context.Context, target oci.Target, opts ProjectOptions) (*releasePlan, error) {
	file, err := project.LoadFile(opts.Dir)
	if err != nil {
		return nil, err
	}

	c, err := project.LoadChart(opts.Dir, file)
	if err != nil {
		return nil, err
	}

	if target.Namespace == "" {
		target.Namespace = file.Namespace
		slog.Debug("using the namespace of the project file", "namespace", target.Namespace)
	}

	specs, err := parseImages(opts.Images, target)
	if err != nil {
		return nil, err
	}

	keys, err := parseImageKeys(opts.ImageKeys, specs)
	if err != nil {
		return nil, err
	}

	copied := make([]string, 0, len(specs))

	for _, spec := range specs {
		copied = append(copied, spec.targetName)

		// An image that replaces the build of an image of cldk.yaml keeps its key, unless --image-key assigns another.
		if key := file.Images[spec.targetName].Key; key != "" && keys[spec.targetName] == "" {
			keys[spec.targetName] = key
		}
	}

	tags, err := project.ReleaseTags(file, opts.Tags, copied)
	if err != nil {
		return nil, err
	}

	builds := project.PlanBuilds(opts.Dir, file, target, tags, copied, opts.Build)

	builder, err := project.CheckBuilder(ctx, r.newProjectBuilder, opts.Build.Builder, builds)
	if err != nil {
		return nil, err
	}

	archive, err := prepareProjectChart(c, builds, specs, keys)
	if err != nil {
		return nil, err
	}

	resolved, err := resolveImages(ctx, specs)
	if err != nil {
		return nil, err
	}

	client, err := oci.NewClient(target)
	if err != nil {
		return nil, err
	}

	built, err := checkBuiltImages(ctx, client, target, builds, opts.Force)
	if err != nil {
		return nil, err
	}

	p, err := checkTargets(ctx, client, resolved, target.ChartRepository(c.Name())+":"+c.Metadata.Version, opts.Force)
	if err != nil {
		return nil, err
	}

	p.archive = archive
	p.builder = builder
	p.builderBinary = opts.Build.Builder
	p.registry = target.Registry
	p.built = built

	return p, nil
}

// newProjectBuilder creates the builder for the build CLI.
func (r *Releaser) newProjectBuilder(binary string) project.Builder {
	return r.newBuilder(binary)
}

// prepareProjectChart points the chart to the targets of the built and the copied images, lints it and returns the packed chart.
func prepareProjectChart(c *helmchart.Chart, builds []project.Image, specs []imageSpec, keys map[string]string) ([]byte, error) {
	targets := make(map[string]string, len(builds)+len(specs))
	allKeys := maps.Clone(keys)

	for _, img := range builds {
		targets[img.Name] = img.Reference()

		if img.Key != "" {
			allKeys[img.Name] = img.Key
		}
	}

	for _, spec := range specs {
		targets[spec.targetName] = spec.target.String()
	}

	if len(targets) > 0 {
		if err := project.RewriteImages(c, targets, allKeys); err != nil {
			return nil, err
		}
	}

	if err := project.Lint(c, nil); err != nil {
		return nil, err
	}

	return chart.Pack(c)
}

// checkBuiltImages checks whether the release tags of the images to build already exist in the target.
func checkBuiltImages(ctx context.Context, client *oci.Client, target oci.Target, builds []project.Image, force bool) ([]checkedBuild, error) {
	checked := make([]checkedBuild, 0, len(builds))

	for _, img := range builds {
		ref, err := name.NewTag(img.Reference(), target.NameOptions()...)
		if err != nil {
			return nil, fmt.Errorf("invalid target %s of image %s: %w", img.Reference(), img.Name, err)
		}

		slog.Debug("checking image target", "target", ref.String())

		existingDigest, exists, err := client.ImageExists(ctx, ref)
		if err != nil {
			return nil, err
		}

		status := StatusPushed

		switch {
		case exists && !force:
			status = StatusConflict
		case exists:
			slog.Debug("overwriting image", "target", ref.String(), "digest", existingDigest)
			status = StatusOverwritten
		}

		checked = append(checked, checkedBuild{Image: img, target: ref, status: status})
	}

	return checked, nil
}

// buildAndPushImages builds all images of cldk.yaml, then pushes them with the builder and returns them.
func (p *releasePlan) buildAndPushImages(ctx context.Context) ([]BuiltImage, error) {
	if len(p.built) == 0 {
		return nil, nil
	}

	builds := make([]project.Image, 0, len(p.built))

	for _, img := range p.built {
		builds = append(builds, img.Image)
	}

	if err := project.BuildImages(ctx, p.builder, builds); err != nil {
		return nil, err
	}

	pushed, pushErr := project.PushImages(ctx, p.builder, builds)
	if pushErr != nil {
		pushErr = fmt.Errorf("%w; built images are pushed with the login of the builder, check it with \"%s login %s\"",
			pushErr, p.builderBinary, p.registry)
	}

	released := make([]BuiltImage, 0, len(pushed))

	for i := range pushed {
		img := p.built[i]

		digest, exists, err := p.client.ImageExists(ctx, img.target)
		if err == nil && !exists {
			err = fmt.Errorf("image %s has been pushed by %s, but is not in the target", img.target, p.builderBinary)
		}

		if err != nil {
			return released, err
		}

		released = append(released, BuiltImage{Name: img.Name, Reference: img.target.String(), Digest: digest, Status: img.status})
	}

	return released, pushErr
}

// planned describes the built image as it would be released.
func (img checkedBuild) planned() BuiltImage {
	return BuiltImage{Name: img.Name, Reference: img.target.String(), Status: img.status}
}
