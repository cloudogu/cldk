package project

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/cloudogu/cldk/internal/cldkfile"
	"github.com/cloudogu/cldk/internal/oci"
)

// ReleaseTags returns the release tag of each image of cldk.yaml to build by image name: the tag of tagSpecs, each
// given as "<name>=<tag>", or, without one, the tag of cldk.yaml. Images copied instead of built need no release tag.
// All images without a release tag are reported in one error.
func ReleaseTags(file cldkfile.ProjectFile, tagSpecs []string, copied []string) (map[string]string, error) {
	tags := map[string]string{}

	for _, spec := range tagSpecs {
		imageName, tag, err := parseTag(spec, file, copied)
		if err != nil {
			return nil, err
		}

		if _, ok := tags[imageName]; ok {
			return nil, fmt.Errorf("image %s is given more than once in --tag", imageName)
		}

		tags[imageName] = tag
	}

	var missing []string

	for _, imageName := range slices.Sorted(maps.Keys(file.Images)) {
		if slices.Contains(copied, imageName) {
			continue
		}

		if _, ok := tags[imageName]; ok {
			continue
		}

		if tag := file.Images[imageName].Tag; tag != "" {
			tags[imageName] = tag

			continue
		}

		missing = append(missing, imageName)
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("no release tag for the images %s, give it with tag: in cldk.yaml or with --tag <name>=<tag>",
			strings.Join(missing, ", "))
	}

	return tags, nil
}

// parseTag parses a release tag given as "<name>=<tag>" for an image of cldk.yaml that is built.
func parseTag(spec string, file cldkfile.ProjectFile, copied []string) (imageName, tag string, err error) {
	imageName, tag, ok := strings.Cut(spec, "=")
	if !ok || imageName == "" || tag == "" {
		return "", "", fmt.Errorf("invalid release tag %q of --tag: must be given as <name>=<tag>, e.g. nexus=3.96.4-1", spec)
	}

	if err := oci.ValidateTag(tag); err != nil {
		return "", "", fmt.Errorf("invalid release tag %q of --tag: %w", spec, err)
	}

	if slices.Contains(copied, imageName) {
		return "", "", fmt.Errorf("release tag %q of --tag is for image %s, which is given with -i and not built", spec, imageName)
	}

	if _, ok := file.Images[imageName]; !ok {
		return "", "", fmt.Errorf("release tag %q of --tag is for no image of cldk.yaml (images: %s)",
			spec, strings.Join(slices.Sorted(maps.Keys(file.Images)), ", "))
	}

	return imageName, tag, nil
}
