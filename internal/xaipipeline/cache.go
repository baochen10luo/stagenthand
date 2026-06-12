package xaipipeline

import (
	"context"
	"os"
)

type FFprobeShotValidator struct{}

func NewFFprobeShotValidator() *FFprobeShotValidator {
	return &FFprobeShotValidator{}
}

func (v *FFprobeShotValidator) ValidShot(ctx context.Context, path string, spec RenderValidationSpec) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}
	if !fileHasMP4FtypMagic(path) {
		return false
	}

	_, err = NewFFprobeOutputValidator().Validate(ctx, path, spec)
	return err == nil
}

func fileHasMP4FtypMagic(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	header := make([]byte, 8)
	if _, err := file.Read(header); err != nil {
		return false
	}
	return string(header[4:8]) == "ftyp"
}
