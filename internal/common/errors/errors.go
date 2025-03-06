package errors

import "errors"

var (
	ErrModulesManifestNameEmpty      = errors.New("manifest name is empty")
	ErrModulesManifestNamespaceEmpty = errors.New("manifest namespace is empty")
)
