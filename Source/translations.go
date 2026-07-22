package main

import (
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"path/filepath"
	"strings"
)

//go:embed strings/*.json
var translationsFS embed.FS

var translations = make(map[string]map[string]string)

var forceLang = flag.String("lang", "", "Force a specific language (e.g. en, es, pt)")

func init() {
	err := fs.WalkDir(translationsFS, "strings", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}

		filename := filepath.Base(path)
		langCodeFull := strings.TrimSuffix(filename, ".json")
		langCode := strings.ToLower(langCodeFull)
		if len(langCode) > 2 {
			langCode = langCode[:2]
		}

		data, err := translationsFS.ReadFile(path)
		if err != nil {
			return err
		}

		var dict map[string]string
		if err := json.Unmarshal(data, &dict); err != nil {
			return err
		}

		translations[langCode] = dict
		return nil
	})
	if err != nil {
		log.Printf("Failed to load translations: %v", err)
	}
}

func getTranslation(lang string, key string) string {
	if *forceLang != "" {
		lang = *forceLang
	}
	langCode := "en"
	if len(lang) >= 2 {
		langCode = strings.ToLower(lang[:2])
	}
	dict, ok := translations[langCode]
	if !ok {
		dict = translations["en"]
	}
	val, ok := dict[key]
	if !ok {
		return translations["en"][key]
	}
	return val
}
