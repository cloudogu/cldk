package chart

// The rewriting of a chart is ported from ces-mirror (core/helm/patchTemplate.go, core/helm/patchTemplateApi.go and
// core/helm/chartManager.go) instead of importing it, so that cldk does not depend on the mirror.

import (
	"bytes"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"text/template"

	"dario.cat/mergo"
	"github.com/google/go-containerregistry/pkg/name"
	"helm.sh/helm/v4/pkg/chart/common"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"
	"sigs.k8s.io/yaml"
)

const (
	// mirrorPatchTemplateFile names the images of a chart and how their references are written into the chart files.
	mirrorPatchTemplateFile = "mirror-patch-tpl.yaml"
	valuesFile              = "values.yaml"
	valuesImagesKey         = "images"
	patchAPIVersionV1       = "v1"
)

// Change is a changed entry of values.images in the mirror patch template.
type Change struct {
	// Key is the key of the entry in values.images.
	Key string
	// Old and New are the image references before and after the rewrite.
	Old string
	New string
}

// patchTemplate is the content of the mirror patch template of a chart.
type patchTemplate struct {
	APIVersion string `json:"apiVersion"`
	// Values are the values the patches are rendered against. The key "images" holds the image references of the chart.
	Values map[string]any `json:"values"`
	// Patches holds, per file of the chart, the values to merge into that file. They are Go templates over Values.
	Patches map[string]any `json:"patches"`
}

// RewriteImages points the images of the chart in place to the targets, given as full references by target name, and
// returns the changed entries of values.images, ordered by target name.
// Each target is assigned to the entry of values.images in the mirror patch template with its key in keys or, without
// one, to the entry whose repository has the target name. The patches of the template are then rendered against the
// changed values and merged into the files of the chart.
func RewriteImages(c *helmchart.Chart, targets map[string]string, keys map[string]string) ([]Change, error) {
	templateFile := findFile(c.Files, mirrorPatchTemplateFile)
	if templateFile == nil {
		return nil, fmt.Errorf("chart has no %s, which is required to release images", mirrorPatchTemplateFile)
	}

	var tpl patchTemplate
	if err := yaml.Unmarshal(templateFile.Data, &tpl); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", mirrorPatchTemplateFile, err)
	}

	images, err := tpl.images()
	if err != nil {
		return nil, err
	}

	changes, err := assignImages(images, targets, keys)
	if err != nil {
		return nil, err
	}

	for _, change := range changes {
		images[change.Key] = change.New
	}

	tpl.Values[valuesImagesKey] = images

	patches, err := tpl.renderPatches()
	if err != nil {
		return nil, err
	}

	for filename, patch := range patches {
		if err = mergeIntoChartFile(c, filename, patch); err != nil {
			return nil, err
		}
	}

	if templateFile.Data, err = yaml.Marshal(tpl); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", mirrorPatchTemplateFile, err)
	}

	return changes, nil
}

// images returns the image references of values.images, keyed by their key.
func (tpl *patchTemplate) images() (map[string]string, error) {
	raw, ok := tpl.Values[valuesImagesKey].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s has no map values.images", mirrorPatchTemplateFile)
	}

	images := make(map[string]string, len(raw))

	for key, ref := range raw {
		s, ok := ref.(string)
		if !ok {
			return nil, fmt.Errorf("values.images.%s in %s is no string", key, mirrorPatchTemplateFile)
		}

		images[key] = s
	}

	return images, nil
}

// assignImages assigns each target to the entry of images with its explicit key or, without one, to the entry whose
// repository has the target name, and returns the resulting changes, ordered by target name.
func assignImages(images map[string]string, targets map[string]string, explicitKeys map[string]string) ([]Change, error) {
	keysByName := map[string][]string{}

	for key, ref := range images {
		parsed, err := name.ParseReference(ref)
		if err != nil {
			return nil, fmt.Errorf("invalid image reference values.images.%s in %s: %w", key, mirrorPatchTemplateFile, err)
		}

		repoName := path.Base(parsed.Context().RepositoryStr())
		keysByName[repoName] = append(keysByName[repoName], key)
	}

	changes := make([]Change, 0, len(targets))
	targetByKey := map[string]string{}

	for _, targetName := range slices.Sorted(maps.Keys(targets)) {
		targetRef := targets[targetName]

		key, err := assignImage(images, keysByName, targetName, targetRef, explicitKeys)
		if err != nil {
			return nil, err
		}

		if other, ok := targetByKey[key]; ok {
			return nil, fmt.Errorf("images %s and %s are both assigned to values.images.%s", other, targetRef, key)
		}

		targetByKey[key] = targetRef
		changes = append(changes, Change{Key: key, Old: images[key], New: targetRef})
	}

	return changes, nil
}

// assignImage returns the key of the entry of images the target is assigned to.
func assignImage(images map[string]string, keysByName map[string][]string, targetName, targetRef string, explicitKeys map[string]string) (string, error) {
	if key, ok := explicitKeys[targetName]; ok {
		if _, ok := images[key]; !ok {
			return "", fmt.Errorf("values.images of %s has no entry %q for image %s", mirrorPatchTemplateFile, key, targetRef)
		}

		return key, nil
	}

	candidates := keysByName[targetName]

	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("no entry in values.images of %s has the repository name %q of image %s; assign one with --image-key %s=<key>",
			mirrorPatchTemplateFile, targetName, targetRef, targetName)
	case 1:
		return candidates[0], nil
	default:
		slices.Sort(candidates)

		return "", fmt.Errorf("several entries in values.images of %s have the repository name %q of image %s: %s; choose one with --image-key %s=<key>",
			mirrorPatchTemplateFile, targetName, targetRef, strings.Join(candidates, ", "), targetName)
	}
}

// renderPatches renders the patches against the values and returns, per file of the chart, the values to merge into
// that file.
func (tpl *patchTemplate) renderPatches() (map[string]map[string]any, error) {
	if tpl.APIVersion != patchAPIVersionV1 {
		return nil, fmt.Errorf("unsupported apiVersion %q in %s", tpl.APIVersion, mirrorPatchTemplateFile)
	}

	raw, err := yaml.Marshal(tpl.Patches)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal patches of %s: %w", mirrorPatchTemplateFile, err)
	}

	goTemplate, err := template.New("patches").Option("missingkey=error").Funcs(patchTemplateFuncs()).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to parse patches of %s: %w", mirrorPatchTemplateFile, err)
	}

	var rendered bytes.Buffer
	if err = goTemplate.Execute(&rendered, tpl.Values); err != nil {
		return nil, fmt.Errorf("failed to render patches of %s: %w", mirrorPatchTemplateFile, err)
	}

	var patches map[string]map[string]any
	if err = yaml.Unmarshal(rendered.Bytes(), &patches); err != nil {
		return nil, fmt.Errorf("failed to parse rendered patches of %s: %w", mirrorPatchTemplateFile, err)
	}

	return patches, nil
}

// patchTemplateFuncs returns the template functions of the patch API v1.
func patchTemplateFuncs() template.FuncMap {
	parse := func(ref string) (name.Reference, error) {
		parsed, err := name.ParseReference(ref)
		if err != nil {
			return nil, fmt.Errorf("invalid image reference %q: %w", ref, err)
		}

		return parsed, nil
	}

	return template.FuncMap{
		"registryFrom": func(ref string) (string, error) {
			parsed, err := parse(ref)
			if err != nil {
				return "", err
			}

			return parsed.Context().RegistryStr(), nil
		},
		"repositoryFrom": func(ref string) (string, error) {
			parsed, err := parse(ref)
			if err != nil {
				return "", err
			}

			return parsed.Context().RepositoryStr(), nil
		},
		"tagFrom": func(ref string) (string, error) {
			parsed, err := parse(ref)
			if err != nil {
				return "", err
			}

			return parsed.Identifier(), nil
		},
	}
}

// mergeIntoChartFile merges the patch into the YAML file of the chart with the given name, overriding existing values.
func mergeIntoChartFile(c *helmchart.Chart, filename string, patch map[string]any) error {
	if filename == valuesFile {
		if c.Values == nil {
			c.Values = map[string]any{}
		}

		if err := mergo.Merge(&c.Values, patch, mergo.WithOverride); err != nil {
			return fmt.Errorf("failed to patch %s: %w", filename, err)
		}

		return writeYAMLFile(c.Raw, filename, c.Values)
	}

	file := findFile(c.Templates, filename)
	if file == nil {
		return fmt.Errorf("failed to patch %s: file not found in chart", filename)
	}

	var content map[string]any
	if err := yaml.Unmarshal(file.Data, &content); err != nil {
		return fmt.Errorf("failed to patch %s: %w", filename, err)
	}

	if content == nil {
		content = map[string]any{}
	}

	if err := mergo.Merge(&content, patch, mergo.WithOverride); err != nil {
		return fmt.Errorf("failed to patch %s: %w", filename, err)
	}

	return writeYAMLFile(c.Templates, filename, content)
}

// writeYAMLFile replaces the content of the file with the given name by the content as YAML.
func writeYAMLFile(files []*common.File, filename string, content map[string]any) error {
	file := findFile(files, filename)
	if file == nil {
		return fmt.Errorf("failed to patch %s: file not found in chart", filename)
	}

	data, err := yaml.Marshal(content)
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", filename, err)
	}

	file.Data = data

	return nil
}

func findFile(files []*common.File, filename string) *common.File {
	for _, f := range files {
		if f != nil && f.Name == filename {
			return f
		}
	}

	return nil
}
