package release

// The rewriting of a chart is ported from ces-mirror (core/helm/patchTemplate.go, core/helm/patchTemplateApi.go and
// core/helm/chartManager.go) instead of importing it, so that cldk does not depend on the mirror.

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strings"
	"text/template"

	"dario.cat/mergo"
	"github.com/google/go-containerregistry/pkg/name"
	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"sigs.k8s.io/yaml"
)

const (
	// mirrorPatchTemplateFile names the images of a chart and how their references are written into the chart files.
	mirrorPatchTemplateFile = "mirror-patch-tpl.yaml"
	valuesFile              = "values.yaml"
	valuesImagesKey         = "images"
	patchAPIVersionV1       = "v1"
)

// imageChange is a changed entry of values.images in the mirror patch template.
type imageChange struct {
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

// rewriteChart points the packed chart archive to the released images and returns the repacked archive.
func rewriteChart(archive []byte, targets map[string]string, explicitKeys map[string]string) ([]byte, []imageChange, error) {
	c, err := loader.LoadArchive(bytes.NewReader(archive))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load chart archive: %w", err)
	}

	templateFile := findFile(c.Files, mirrorPatchTemplateFile)
	if templateFile == nil {
		return nil, nil, fmt.Errorf("chart has no %s, which is required to release images", mirrorPatchTemplateFile)
	}

	var tpl patchTemplate
	if err = yaml.Unmarshal(templateFile.Data, &tpl); err != nil {
		return nil, nil, fmt.Errorf("failed to parse %s: %w", mirrorPatchTemplateFile, err)
	}

	images, err := tpl.images()
	if err != nil {
		return nil, nil, err
	}

	changes, err := assignImages(images, targets, explicitKeys)
	if err != nil {
		return nil, nil, err
	}

	for _, change := range changes {
		images[change.Key] = change.New
	}

	tpl.Values[valuesImagesKey] = images

	patches, err := tpl.renderPatches()
	if err != nil {
		return nil, nil, err
	}

	for filename, patch := range patches {
		if err = mergeIntoChartFile(c, filename, patch); err != nil {
			return nil, nil, err
		}
	}

	if templateFile.Data, err = yaml.Marshal(tpl); err != nil {
		return nil, nil, fmt.Errorf("failed to write %s: %w", mirrorPatchTemplateFile, err)
	}

	repacked, err := packArchive(c)
	if err != nil {
		return nil, nil, err
	}

	return repacked, changes, nil
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
func assignImages(images map[string]string, targets map[string]string, explicitKeys map[string]string) ([]imageChange, error) {
	keysByName := map[string][]string{}

	for key, ref := range images {
		parsed, err := name.ParseReference(ref)
		if err != nil {
			return nil, fmt.Errorf("invalid image reference values.images.%s in %s: %w", key, mirrorPatchTemplateFile, err)
		}

		repoName := path.Base(parsed.Context().RepositoryStr())
		keysByName[repoName] = append(keysByName[repoName], key)
	}

	changes := make([]imageChange, 0, len(targets))
	targetByKey := map[string]string{}

	for _, targetName := range slices.Sorted(maps.Keys(targets)) {
		key, err := assignImage(images, keysByName, targetName, targets[targetName], explicitKeys)
		if err != nil {
			return nil, err
		}

		if other, ok := targetByKey[key]; ok {
			return nil, fmt.Errorf("images %s and %s are both assigned to values.images.%s", other, targets[targetName], key)
		}

		targetByKey[key] = targets[targetName]
		changes = append(changes, imageChange{Key: key, Old: images[key], New: targets[targetName]})
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
	slices.Sort(candidates)

	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("no entry in values.images of %s has the repository name %q of image %s; assign one with --image-key %s=<key>",
			mirrorPatchTemplateFile, targetName, targetRef, targetName)
	case 1:
		return candidates[0], nil
	default:
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

	goTemplate, err := template.New("patches").Funcs(patchTemplateFuncs()).Parse(string(raw))
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
func mergeIntoChartFile(c *chart.Chart, filename string, patch map[string]any) error {
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

// packArchive packs the chart like "helm package" does and returns the archive.
func packArchive(c *chart.Chart) (archive []byte, err error) {
	dir, err := os.MkdirTemp("", "cldk-chart-")
	if err != nil {
		return nil, fmt.Errorf("failed to pack chart: %w", err)
	}

	defer func() {
		err = errors.Join(err, os.RemoveAll(dir))
	}()

	archivePath, err := chartutil.Save(c, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to pack chart: %w", err)
	}

	archive, err = os.ReadFile(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to pack chart: %w", err)
	}

	return archive, nil
}
