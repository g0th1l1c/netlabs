package server

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Лимит длины имени в байтах,обычно допускают 255 байт, запас оставлен под суффикс "_N" при совпадении имён
const maxStoredNameBytes = 240

//превращает имя от клиента в безопасное имя файла
func sanitizeName(name string) string {
	// отбрасывает любые пути
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	//заменяем запрещённые символы
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)

	for len(name) > maxStoredNameBytes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}

	// удаляет все точки и пробелы в конце строки
	name = strings.TrimRight(name, " .")
	if name == "" {
		name = "file"
	}
	return name
}

//создаёт новый файл в dir
func createUnique(dir, name string) (*os.File, string, error) {
	name = sanitizeName(name)
	ext := filepath.Ext(name)             //расширение
	base := strings.TrimSuffix(name, ext) // убираем расширение
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, "", fmt.Errorf("каталог uploads должен быть обычной директорией")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, "", err
	}
	defer root.Close()

	for i := 0; i < 10000; i++ {
		candidate := name
		if i > 0 {
			suffix := fmt.Sprintf("_%d", i)
			storedExt := trimUTF8(ext, maxStoredNameBytes-len(suffix))
			candidate = trimUTF8(base, maxStoredNameBytes-len(storedExt)-len(suffix)) + suffix + storedExt
		}
		p := filepath.Join(dir, candidate) // полный путь к файлу

		f, err := root.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, p, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("не удалось подобрать свободное имя файла")
}

func trimUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	for len(value) > maxBytes {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}
