package devpush

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/cloudogu/cldk/internal/cldkfile"
	"github.com/cloudogu/cldk/internal/oci"
	"github.com/cloudogu/cldk/internal/project"
)

// PushedImage is a built and pushed image.
type PushedImage struct {
	// Name is the image name in cldk.yaml, e.g. "nexus".
	Name string
	// Reference is the reference the image has been pushed to, e.g.
	// "localhost:5001/official/dogu/v3/images/nexus:1.2.3-dev.20261103143012".
	Reference string
}

// ReplacedImage is an image written into the chart with a given reference instead of being built.
type ReplacedImage struct {
	// Name is the image name in cldk.yaml, e.g. "sidecar".
	Name string
	// Reference is the reference written into the chart, e.g. "registry.example.com/foo:1.0".
	Reference string
}

// imagePlan is what happens to the images of the project file.
type imagePlan struct {
	// built are the images to build and push, ordered by name.
	built []project.Image
	// replaced are the images written into the chart with a given reference instead of being built, ordered by name.
	replaced []ReplacedImage
	// skipped are the names of the images that are neither built nor written into the chart, ordered by name.
	skipped []string
	// chartRefs are the references written into the chart by image name: the push reference on the cluster address
	// for a built image, the given reference for a replaced image.
	chartRefs map[string]string
	// keys are the explicit keys in values.images of the mirror patch template by image name, if any.
	keys map[string]string
}

// pushed returns the built images as pushed images.
func (p imagePlan) pushed() []PushedImage {
	pushed := make([]PushedImage, 0, len(p.built))

	for _, img := range p.built {
		pushed = append(pushed, PushedImage{Name: img.Name, Reference: img.Reference()})
	}

	return pushed
}

// checkImageNames checks that the images to replace and to skip are images of the project file.
func checkImageNames(opts Options, file cldkfile.ProjectFile) error {
	known := strings.Join(slices.Sorted(maps.Keys(file.Images)), ", ")

	for _, imageName := range slices.Sorted(maps.Keys(opts.ReplaceImages)) {
		if _, ok := file.Images[imageName]; !ok {
			return fmt.Errorf("unknown image %q of --replace-image, cldk.yaml has the images %s", imageName, known)
		}
	}

	for _, imageName := range opts.SkipImages {
		if _, ok := file.Images[imageName]; !ok {
			return fmt.Errorf("unknown image %q of --skip-image, cldk.yaml has the images %s", imageName, known)
		}
	}

	return nil
}

// planImages returns what happens to the images of the project file, ordered by name:
//   - skipped images are left out
//   - replaced images keep their given reference
//   - other images are built with the dev version as tag
func planImages(opts Options, file cldkfile.ProjectFile, target, clusterTarget oci.Target, version string) imagePlan {
	plan := imagePlan{chartRefs: map[string]string{}, keys: map[string]string{}}
	excluded := slices.Clone(opts.SkipImages)
	tags := make(map[string]string, len(file.Images))

	for _, imageName := range slices.Sorted(maps.Keys(file.Images)) {
		tags[imageName] = version

		if slices.Contains(opts.SkipImages, imageName) {
			slog.Info("skipping image", "name", imageName)
			plan.skipped = append(plan.skipped, imageName)

			continue
		}

		if key := file.Images[imageName].Key; key != "" {
			plan.keys[imageName] = key
		}

		if ref, ok := opts.ReplaceImages[imageName]; ok {
			slog.Info("replacing image", "name", imageName, "reference", ref)
			plan.replaced = append(plan.replaced, ReplacedImage{Name: imageName, Reference: ref})
			plan.chartRefs[imageName] = ref
			excluded = append(excluded, imageName)

			continue
		}

		plan.chartRefs[imageName] = clusterTarget.ImageRepository(imageName) + ":" + version
	}

	plan.built = project.PlanBuilds(opts.Dir, file, target, tags, excluded, opts.Build)

	return plan
}
