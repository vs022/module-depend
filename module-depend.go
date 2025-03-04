package main

import (
	"debug/elf"
	"debug/pe"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type moduleName struct {
	name     string
	caseless bool
}

type appEnv struct {
	module  []string
	fromDir []string
}

const pathSplitter = ","

func main() {
	defer checkPanic()

	app := parseArgv()
	imports := getImports(app.module)
	if len(app.fromDir) == 0 {
		sort.Slice(imports, func(i, j int) bool {
			return imports[i].name < imports[j].name
		})
		printValues(imports)
	} else {
		files := listFilesFromDir(app.fromDir)
		deps := listDepsFromDir(imports, files)
		sort.Strings(deps)
		printValues(deps)
	}
}

func parseArgv() *appEnv {
	var app appEnv
	var path string

	fl := flag.NewFlagSet("", flag.ContinueOnError)
	fl.StringVar(&path, "from-dir", "", "directories to get dependencies from")
	if err := fl.Parse(os.Args[1:]); err != nil {
		panic("")
	}

	app.module = fl.Args()
	if path != "" {
		app.fromDir = strings.Split(path, pathSplitter)
	}
	return &app
}

func getImports(modules []string) []moduleName {
	var imports []moduleName
	for _, mod := range modules {
		if mod == "" {
			continue
		}
		appendImports(&imports, mod)
	}
	return imports
}

func listDepsFromDir(modules []moduleName, files []string) []string {
	var deps []string
	for i := 0; i < len(modules); i++ {
		f := findModFile(files, modules[i])
		if f >= 0 {
			appendImports(&modules, files[f])
			deps = append(deps, files[f])
		}
	}
	return deps
}

func appendImports(imp *[]moduleName, modPath string) {
	if !isFile(modPath) {
		panic("Not a file: '" + modPath + "'")
	}
	if appendImportsELF(imp, modPath) {
		return
	}
	if appendImportsPE(imp, modPath) {
		return
	}
	panic("Unknown module type: '" + modPath + "'")
}

func appendImportsELF(imp *[]moduleName, modPath string) bool {
	f, err := elf.Open(modPath)
	if err != nil {
		return false
	}
	defer f.Close()
	modImp, err := f.ImportedLibraries()
	if err != nil {
		return false
	}
	appendModImp(imp, modImp, false)
	return true
}

func appendImportsPE(imp *[]moduleName, modPath string) bool {
	f, err := pe.Open(modPath)
	if err != nil {
		return false
	}
	defer f.Close()

	/* ImportedLibraries() is a stub */
	if f.OptionalHeader == nil {
		return false
	}
	// grab the number of data directory entries
	var dd_length uint32
	if _, pe64 := f.OptionalHeader.(*pe.OptionalHeader64); pe64 {
		dd_length = f.OptionalHeader.(*pe.OptionalHeader64).NumberOfRvaAndSizes
	} else {
		dd_length = f.OptionalHeader.(*pe.OptionalHeader32).NumberOfRvaAndSizes
	}
	// check that the length of data directory entries is large
	// enough to include the imports directory.
	if dd_length < pe.IMAGE_DIRECTORY_ENTRY_IMPORT+1 {
		return true
	}
	// grab the import data directory entry
	var idd pe.DataDirectory
	if _, pe64 := f.OptionalHeader.(*pe.OptionalHeader64); pe64 {
		idd = f.OptionalHeader.(*pe.OptionalHeader64).DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_IMPORT]
	} else {
		idd = f.OptionalHeader.(*pe.OptionalHeader32).DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_IMPORT]
	}
	// figure out which section contains the import directory table
	var ds *pe.Section
	for _, s := range f.Sections {
		if s.VirtualAddress <= idd.VirtualAddress && idd.VirtualAddress < s.VirtualAddress+s.VirtualSize {
			ds = s
			break
		}
	}
	// didn't find a section, so no import libraries were found
	if ds == nil {
		return true
	}
	data, err := ds.Data()
	if err != nil {
		return false
	}
	// seek to the virtual address specified in the import data directory
	var modImp []string
	d := data[idd.VirtualAddress-ds.VirtualAddress:]
	for len(d) >= 20 {
		originalFirstThunk := binary.LittleEndian.Uint32(d[0:4])
		if originalFirstThunk == 0 {
			break
		}
		name := binary.LittleEndian.Uint32(d[12:16])
		if mod, ok := getStringPE(data, int(name-ds.VirtualAddress)); ok {
			modImp = append(modImp, mod)
		}
		d = d[20:]
	}

	appendModImp(imp, modImp, true)
	return true
}

func getStringPE(section []byte, start int) (string, bool) {
	if start < 0 || start >= len(section) {
		return "", false
	}
	for end := start; end < len(section); end++ {
		if section[end] == 0 {
			return string(section[start:end]), true
		}
	}
	return "", false
}

func appendModImp(imp *[]moduleName, modImp []string, caseless bool) {
	for _, m := range modImp {
		appendIfNew(imp, m, caseless)
	}
}

func appendIfNew(imp *[]moduleName, modImp string, caseless bool) {
	var mi string
	if caseless {
		mi = strings.ToUpper(modImp)
	} else {
		mi = modImp
	}
	found := false
	for _, m := range *imp {
		var name string
		if caseless {
			name = strings.ToUpper(m.name)
		} else {
			name = m.name
		}
		if mi == name {
			found = true
			break
		}
	}
	if !found {
		*imp = append(*imp,
			moduleName{
				name:     modImp,
				caseless: caseless})
	}
}

func listFilesFromDir(dirs []string) []string {
	var files []string
	var level uint
	for _, path := range dirs {
		if path == "" {
			continue
		}
		level = 0
		walkDir(&files, path, &level)
	}
	return files
}

func walkDir(files *[]string, path string, level *uint) {
	if *level > 1024 {
		panic("Too many directory recursion levels")
	}
	stat, err := os.Stat(path)
	if err != nil {
		if *level == 0 {
			panic(err.Error())
		}
		return
	}
	mode := stat.Mode()
	if mode.IsDir() {
		lsDir, err := os.ReadDir(path)
		if err != nil {
			if *level == 0 && len(lsDir) == 0 {
				panic(err.Error())
			}
		}
		for _, lsItem := range lsDir {
			name := lsItem.Name()
			if name == "." || name == ".." {
				continue
			}
			(*level)++
			walkDir(files, filepath.Join(path, name), level)
			(*level)--
		}
	} else if mode.IsRegular() {
		*files = append(*files, path)
	} else {
		if *level == 0 {
			panic("Not a regular file or directory: '" + path + "'")
		}
	}
}

func findModFile(files []string, mod moduleName) int {
	var i int
	var path string
	var eq bool
	for i, path = range files {
		name := filepath.Base(path)
		if mod.caseless {
			eq = strings.ToUpper(name) == strings.ToUpper(mod.name)
		} else {
			eq = name == mod.name
		}
		if eq {
			return i
		}
	}
	return -1
}

func isFile(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.Mode().IsRegular()
}

func (mod moduleName) String() string {
	return mod.name
}

func printValues[T any](val []T) {
	for _, v := range val {
		fmt.Println(v)
	}
}
