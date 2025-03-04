# Module-depend

Get dependencies for [ELF](https://en.wikipedia.org/wiki/Executable_and_Linkable_Format) and [PE](https://en.wikipedia.org/wiki/Portable_Executable) modules.

## Usage
```
module-depend [module1 module2 ...]
```
will print direct dependencies (SO or DLL) for modules.

To recursively obtain dependencies provide directories with `-from-dir`:
```
module-depend -from-dir dir1,dir2,... [modules ...]
```

## Build
```
git clone https://github.com/vs022/module-depend.git
cd module-depend
go mod init module-depend
go build
```
