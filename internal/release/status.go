package release

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudogu/cldk/internal/oci"
)

// checkImages checks the targets of all images and returns the images with their status.
func checkImages(ctx context.Context, client *oci.Client, images []resolvedImage, force bool) ([]checkedImage, error) {
	checked := make([]checkedImage, 0, len(images))

	for _, img := range images {
		c, err := checkImage(ctx, client, img, force)
		if err != nil {
			return nil, err
		}

		checked = append(checked, c)
	}

	return checked, nil
}

// checkImage checks whether the target tag of the image already exists and returns the image with its status.
// An existing tag with another digest is a conflict unless force allows to overwrite it.
func checkImage(ctx context.Context, client *oci.Client, img resolvedImage, force bool) (checkedImage, error) {
	slog.Debug("checking image target", "target", img.target.String())

	existingDigest, exists, err := client.ImageExists(ctx, img.target)
	if err != nil {
		return checkedImage{}, err
	}

	if !exists {
		return checkedImage{resolvedImage: img, status: StatusPushed}, nil
	}

	checked := checkedImage{resolvedImage: img, existingDigest: existingDigest}

	switch {
	case existingDigest == img.desc.Digest.String():
		checked.status = StatusSkipped
	case !force:
		checked.status = StatusConflict
	default:
		slog.Debug("overwriting image", "target", img.target.String(), "digest", existingDigest)
		checked.status = StatusOverwritten
	}

	return checked, nil
}

// pushCheckedImage copies the manifest or index of the image unchanged to its target, unless the target already has it.
func pushCheckedImage(ctx context.Context, client *oci.Client, img checkedImage) (ReleasedImage, error) {
	released := img.released()

	if released.Status == StatusSkipped {
		slog.Debug("skipping already released image", "source", img.source.String(), "target", img.target.String())

		return released, nil
	}

	slog.Debug("pushing image", "source", img.source.String(), "target", img.target.String())

	if err := client.PushImage(ctx, img.target, img.desc); err != nil {
		return ReleasedImage{}, fmt.Errorf("failed to push image %s to %s: %w", img.source, img.target, err)
	}

	return released, nil
}

// checkChart returns the status the chart gets in the target.
// An already released chart version is a conflict unless force allows to overwrite it.
// The digests are not compared, because rewriting and packing the chart changes its digest.
func checkChart(ctx context.Context, client *oci.Client, ref string, force bool) (Status, error) {
	slog.Debug("checking chart target", "target", ref)

	existingDigest, exists, err := client.ChartExists(ctx, ref)
	if err != nil {
		return StatusPushed, err
	}

	if !exists {
		return StatusPushed, nil
	}

	if !force {
		return StatusConflict, nil
	}

	slog.Debug("overwriting chart", "target", ref, "digest", existingDigest)

	return StatusOverwritten, nil
}
