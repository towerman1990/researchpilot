package task

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidMaterials = errors.New("invalid_research_materials")

const (
	maxMaterials     = 2
	maxMaterialBytes = 24 << 10
)

type Material struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func normalizeMaterials(input []Material) ([]Material, error) {
	if len(input) > maxMaterials {
		return nil, ErrInvalidMaterials
	}
	files := make([]Material, 0, len(input))
	seen := make(map[[32]byte]bool, len(input))
	for _, file := range input {
		file, err := NormalizeMaterial(file)
		if err != nil {
			return nil, err
		}
		key := materialFingerprint(file)
		if seen[key] {
			return nil, ErrInvalidMaterials
		}
		seen[key] = true
		files = append(files, file)
	}
	return files, nil
}

func NormalizeMaterial(file Material) (Material, error) {
	file.Name = strings.TrimSpace(file.Name)
	file.Content = strings.TrimSpace(strings.TrimPrefix(file.Content, "\ufeff"))
	extension := strings.ToLower(path.Ext(file.Name))
	if (extension != ".txt" && extension != ".md") || len(file.Name) > 240 ||
		strings.ContainsAny(file.Name, "/\\") || strings.IndexFunc(file.Name, unicode.IsControl) >= 0 ||
		file.Content == "" || strings.IndexByte(file.Content, 0) >= 0 || !utf8.ValidString(file.Content) || len(file.Content) > maxMaterialBytes {
		return Material{}, ErrInvalidMaterials
	}
	return file, nil
}

func materialFingerprint(file Material) [32]byte {
	raw, _ := json.Marshal(file)
	return sha256.Sum256(append([]byte{0xfc}, raw...))
}
