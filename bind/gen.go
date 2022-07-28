// Copyright 2019 The go-python Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bind

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// this version uses pybindgen and a generated .go file to do the binding

const (
	// GoHandle is the type to use for the Handle map key, go-side
	GoHandle = "int64"
	// CGoHandle is Handle for cgo files
	CGoHandle = "C.longlong"
	// PyHandle is within python
	PyHandle = "int64_t"
)

type BuildMode string

const (
	ModeGen   BuildMode = "gen"
	ModeBuild           = "build"
	ModeExe             = "exe"
	ModePkg             = "pkg"
)

// set this to true if OS is windows
var WindowsOS = false

// for all preambles: 1 = name of package (outname), 2 = cmdstr

// 3 = libcfg, 4 = GoHandle, 5 = CGoHandle, 6 = all imports, 7 = mainstr, 8 = exe pre C, 9 = exe pre go
const (
	goPreamble = `/*
cgo stubs for package %[1]s.
File is generated by gopy. Do not edit.
%[2]s
*/

package main

/*
%[3]s
// #define Py_LIMITED_API // need full API for PyRun*
#include <Python.h>
typedef uint8_t bool;
// static inline is trick for avoiding need for extra .c file
// the following are used for build value -- switch on reflect.Kind
// or the types equivalent
static inline PyObject* gopy_build_bool(uint8_t val) {
	return Py_BuildValue("b", val);
}
static inline PyObject* gopy_build_int64(int64_t val) {
	return Py_BuildValue("k", val);
}
static inline PyObject* gopy_build_uint64(uint64_t val) {
	return Py_BuildValue("K", val);
}
static inline PyObject* gopy_build_float64(double val) {
	return Py_BuildValue("d", val);
}
static inline PyObject* gopy_build_string(const char* val) {
	return Py_BuildValue("s", val);
}
static inline void gopy_decref(PyObject* obj) { // macro
	Py_XDECREF(obj);
}
static inline void gopy_incref(PyObject* obj) { // macro
	Py_XINCREF(obj);
}
static inline int gopy_method_check(PyObject* obj) { // macro
	return PyMethod_Check(obj);
}
static inline void gopy_err_handle() {
	if(PyErr_Occurred() != NULL) {
		PyErr_Print();
	}
}
static PyObject* Py_BuildValue1(char *format, void* arg0)
{
	PyObject *retval = Py_BuildValue(format, arg0);
	free(format);
	return retval;
}
%[8]s
*/
import "C"
import (
	"github.com/go-python/gopy/gopyh" // handler
	%[6]s
)

// main doesn't do anything in lib / pkg mode, but is essential for exe mode
func main() {
	%[7]s
}

// initialization functions -- can be called from python after library is loaded
// GoPyInitRunFile runs a separate python file -- call in GoPyInit if it
// steals the main thread e.g., for GUI event loop, as in GoGi startup.

//export GoPyInit
func GoPyInit() {
	%[7]s
}

// type for the handle -- int64 for speed (can switch to string)
type GoHandle %[4]s
type CGoHandle %[5]s

// DecRef decrements the reference count for the specified handle
// and deletes it it goes to zero.
//export DecRef
func DecRef(handle CGoHandle) {
	gopyh.DecRef(gopyh.CGoHandle(handle))
}

// IncRef increments the reference count for the specified handle.
//export IncRef
func IncRef(handle CGoHandle) {
	gopyh.IncRef(gopyh.CGoHandle(handle))
}

// NumHandles returns the number of handles currently in use.
//export NumHandles
func NumHandles() int {
	return gopyh.NumHandles()
}

// boolGoToPy converts a Go bool to python-compatible C.char
func boolGoToPy(b bool) C.char {
	if b {
		return 1
	}
	return 0
}

// boolPyToGo converts a python-compatible C.Char to Go bool
func boolPyToGo(b C.char) bool {
	if b != 0 {
		return true
	}
	return false
}

func complex64GoToPy(c complex64) *C.PyObject {
	return C.PyComplex_FromDoubles(C.double(real(c)), C.double(imag(c)))
}

func complex64PyToGo(o *C.PyObject) complex64 {
	v := C.PyComplex_AsCComplex(o)
	return complex(float32(v.real), float32(v.imag))
}

func complex128GoToPy(c complex128) *C.PyObject {
	return C.PyComplex_FromDoubles(C.double(real(c)), C.double(imag(c)))
}

func complex128PyToGo(o *C.PyObject) complex128 {
	v := C.PyComplex_AsCComplex(o)
	return complex(float64(v.real), float64(v.imag))
}

%[9]s
`

	goExePreambleC = `
#if PY_VERSION_HEX >= 0x03000000
extern PyObject* PyInit__%[1]s(void);
static inline void gopy_load_mod() {
	PyImport_AppendInittab("_%[1]s", PyInit__%[1]s);
}
#else
extern void* init__%[1]s(void);
static inline void gopy_load_mod() {
	PyImport_AppendInittab("_%[1]s", init__%[1]s);
}
#endif

`

	goExePreambleGo = `
// wchar version of startup args
var wargs []*C.wchar_t

//export GoPyMainRun
func GoPyMainRun() {
	// need to encode char* into wchar_t*
	for i := range os.Args {
		cstr := C.CString(os.Args[i])
		wargs = append(wargs, C.Py_DecodeLocale(cstr, nil))
		C.free(unsafe.Pointer(cstr))
	}
	C.gopy_load_mod()
	C.Py_Initialize()
	C.PyEval_InitThreads()
	C.Py_Main(C.int(len(wargs)), &wargs[0])
}

`

	PyBuildPreamble = `# python build stubs for package %[1]s
# File is generated by gopy. Do not edit.
# %[2]s

from pybindgen import retval, param, Function, Module
import sys

class CheckedFunction(Function):
    def __init__(self, *a, **kw):
        super(CheckedFunction, self).__init__(*a, **kw)
        self._failure_expression = kw.get('failure_expression', '')
        self._failure_cleanup = kw.get('failure_cleanup', '')

    def set_failure_expression(self, expr):
        self._failure_expression = expr

    def set_failure_cleanup(self, expr):
        self._failure_cleanup = expr

    def generate_call(self):
        super(CheckedFunction, self).generate_call()
        check = "PyErr_Occurred()"
        if self._failure_expression:
            check = "{} && {}".format(self._failure_expression, check)
        failure_cleanup = self._failure_cleanup or None
        self.before_call.write_error_check(check, failure_cleanup)

def add_checked_function(mod, name, retval, params, failure_expression='', *a, **kw):
    fn = CheckedFunction(name, retval, params, *a, **kw)
    fn.set_failure_expression(failure_expression)
    mod._add_function_obj(fn)
    return fn

def add_checked_string_function(mod, name, retval, params, failure_expression='', *a, **kw):
    fn = CheckedFunction(name, retval, params, *a, **kw)
    fn.set_failure_cleanup('if (retval != NULL) free(retval);')
    fn.after_call.add_cleanup_code('free(retval);')
    fn.set_failure_expression(failure_expression)
    mod._add_function_obj(fn)
    return fn

mod = Module('_%[1]s')
mod.add_include('"%[1]s_go.h"')
mod.add_function('GoPyInit', None, [])
mod.add_function('DecRef', None, [param('int64_t', 'handle')])
mod.add_function('IncRef', None, [param('int64_t', 'handle')])
mod.add_function('NumHandles', retval('int'), [])
`

	// appended to imports in py wrap preamble as key for adding at end
	importHereKeyString = "%%%%%%<<<<<<ADDIMPORTSHERE>>>>>>>%%%%%%%"

	// 3 = specific package name, 4 = spec pkg path, 5 = doc, 6 = imports
	PyWrapPreamble = `%[5]s
# python wrapper for package %[4]s within overall package %[1]s
# This is what you import to use the package.
# File is generated by gopy. Do not edit.
# %[2]s

# the following is required to enable dlopen to open the _go.so file
import os,sys,inspect,collections
try:
	import collections.abc as _collections_abc
except ImportError:
	_collections_abc = collections

cwd = os.getcwd()
currentdir = os.path.dirname(os.path.abspath(inspect.getfile(inspect.currentframe())))
os.chdir(currentdir)
%[6]s
os.chdir(cwd)

# to use this code in your end-user python file, import it as follows:
# from %[1]s import %[3]s
# and then refer to everything using %[3]s. prefix
# packages imported by this package listed below:

%[7]s

`

	// exe version of preamble -- doesn't need complex code to load _ module
	// 3 = specific package name, 4 = spec pkg path, 5 = doc, 6 = imports
	PyWrapExePreamble = `%[5]s
# python wrapper for package %[4]s within standalone executable package %[1]s
# This is what you import to use the package.
# File is generated by gopy. Do not edit.
# %[2]s

import collections
try:
	import collections.abc as _collections_abc
except ImportError:
	_collections_abc = collections
%[6]s

# to use this code in your end-user python file, import it as follows:
# from %[1]s import %[3]s
# and then refer to everything using %[3]s. prefix
# packages imported by this package listed below:

%[7]s

`

	GoPkgDefs = `
import collections
try:
	import collections.abc as _collections_abc
except ImportError:
	_collections_abc = collections
	
class GoClass(object):
	"""GoClass is the base class for all GoPy wrapper classes"""
	def __init__(self):
		self.handle = 0

# use go.nil for nil pointers 
nil = GoClass()

# need to explicitly initialize it
def main():
	global nil
	nil = GoClass()

main()

def Init():
	"""calls the GoPyInit function, which runs the 'main' code string that was passed using -main arg to gopy"""
	_%[1]s.GoPyInit()

	`

	// 3 = gencmd, 4 = vm, 5 = libext 6 = extraGccArgs, 7 = CFLAGS, 8 = LDLFAGS,
	// 9 = windows special declspec hack
	MakefileTemplate = `# Makefile for python interface for package %[1]s.
# File is generated by gopy. Do not edit.
# %[2]s

GOCMD=go
GOBUILD=$(GOCMD) build -mod=mod
GOIMPORTS=goimports
PYTHON=%[4]s
LIBEXT=%[5]s

# get the CC and flags used to build python:
GCC = $(shell $(GOCMD) env CC)
CFLAGS = %[7]s
LDFLAGS = %[8]s

all: gen build

gen:
	%[3]s

build:
	# build target builds the generated files -- this is what gopy build does..
	# this will otherwise be built during go build and may be out of date
	- rm %[1]s.c
	# goimports is needed to ensure that the imports list is valid
	$(GOIMPORTS) -w %[1]s.go
	# generate %[1]s_go$(LIBEXT) from %[1]s.go -- the cgo wrappers to go functions
	$(GOBUILD) -buildmode=c-shared -o %[1]s_go$(LIBEXT) %[1]s.go
	# use pybindgen to build the %[1]s.c file which are the CPython wrappers to cgo wrappers..
	# note: pip install pybindgen to get pybindgen if this fails
	$(PYTHON) build.py
	# build the _%[1]s$(LIBEXT) library that contains the cgo and CPython wrappers
	# generated %[1]s.py python wrapper imports this c-code package
	%[9]s
	$(GCC) %[1]s.c %[6]s %[1]s_go$(LIBEXT) -o _%[1]s$(LIBEXT) $(CFLAGS) $(LDFLAGS) -fPIC --shared -w
	
`

	// exe version of template: 3 = gencmd, 4 = vm, 5 = libext
	MakefileExeTemplate = `# Makefile for python interface for standalone executable package %[1]s.
# File is generated by gopy. Do not edit.
# %[2]s

GOCMD=go
GOBUILD=$(GOCMD) build -mod=mod
GOIMPORTS=goimports
PYTHON=%[4]s
LIBEXT=%[5]s
CFLAGS = %[6]s
LDFLAGS = %[7]s

# get the flags used to build python:
GCC = $(shell $(GOCMD) env CC)

all: gen build

gen:
	%[3]s

build:
	# build target builds the generated files into exe -- this is what gopy build does..
	# goimports is needed to ensure that the imports list is valid
	$(GOIMPORTS) -w %[1]s.go
	# this will otherwise be built during go build and may be out of date
	- rm %[1]s.c 
	echo "typedef uint8_t bool;" > %[1]s_go.h
	# this will fail but is needed to generate the .c file that then allows go build to work
	- $(PYTHON) build.py >/dev/null 2>&1
	# generate %[1]s_go.h from %[1]s.go -- unfortunately no way to build .h only
	$(GOBUILD) -buildmode=c-shared -o %[1]s_go$(LIBEXT)
	# use pybindgen to build the %[1]s.c file which are the CPython wrappers to cgo wrappers..
	# note: pip install pybindgen to get pybindgen if this fails
	$(PYTHON) build.py
	# build the executable
	- rm %[1]s_go$(LIBEXT)
	$(GOBUILD) -o py%[1]s
	
`
)

// thePyGen is the current pyGen which is needed in symbols to lookup
// package paths -- not very clean to pass around or add to various
// data structures to make local, but if that ends up being critical
// for some reason, it could be done.
var thePyGen *pyGen

// NoWarn turns off warnings -- this must be a global as it is relevant during initial package parsing
// before e.g., thePyGen is present.
var NoWarn = false

// NoMake turns off generation of Makefiles
var NoMake = false

// GenPyBind generates a .go file, build.py file to enable pybindgen to create python bindings,
// and wrapper .py file(s) that are loaded as the interface to the package with shadow
// python-side classes
// mode = gen, build, pkg, exe
func GenPyBind(mode BuildMode, libext, extragccargs string, lang int, cfg *BindCfg) error {
	gen := &pyGen{
		mode:         mode,
		pypkgname:    cfg.Name,
		cfg:          cfg,
		libext:       libext,
		extraGccArgs: extragccargs,
		lang:         lang,
	}
	gen.genPackageMap()
	thePyGen = gen
	err := gen.gen()
	thePyGen = nil
	if err != nil {
		return err
	}
	return err
}

type pyGen struct {
	gofile   *printer
	leakfile *printer
	pybuild  *printer
	pywrap   *printer
	makefile *printer

	pkg    *Package // current package (only set when doing package-specific processing)
	err    ErrorList
	pkgmap map[string]struct{} // map of package paths

	mode         BuildMode // mode: gen, build, pkg, exe
	pypkgname    string
	cfg          *BindCfg
	libext       string
	extraGccArgs string
	lang         int // c-python api version (2,3)
}

func (g *pyGen) gen() error {
	g.pkg = nil
	err := os.MkdirAll(g.cfg.OutputDir, 0755)
	if err != nil {
		return fmt.Errorf("gopy: could not create output directory: %v", err)
	}

	g.genPre()
	g.genExtTypesGo()
	for _, p := range Packages {
		g.genPkg(p)
	}
	g.genOut()
	if len(g.err) == 0 {
		return nil
	}
	return g.err.Error()
}

func (g *pyGen) genPackageMap() {
	g.pkgmap = make(map[string]struct{})
	for _, p := range Packages {
		g.pkgmap[p.pkg.Path()] = struct{}{}
	}
}

func (g *pyGen) genPre() {
	g.gofile = &printer{buf: new(bytes.Buffer), indentEach: []byte("\t")}
	g.leakfile = &printer{buf: new(bytes.Buffer), indentEach: []byte("\t")}
	g.pybuild = &printer{buf: new(bytes.Buffer), indentEach: []byte("\t")}
	if !NoMake {
		g.makefile = &printer{buf: new(bytes.Buffer), indentEach: []byte("\t")}
	}
	g.genGoPreamble()
	g.genPyBuildPreamble()
	if !NoMake {
		g.genMakefile()
	}
	oinit, err := os.Create(filepath.Join(g.cfg.OutputDir, "__init__.py"))
	g.err.Add(err)
	err = oinit.Close()
	g.err.Add(err)
}

func (g *pyGen) genPrintOut(outfn string, pr *printer) {
	of, err := os.Create(filepath.Join(g.cfg.OutputDir, outfn))
	g.err.Add(err)
	_, err = io.Copy(of, pr)
	g.err.Add(err)
	err = of.Close()
	g.err.Add(err)
}

func (g *pyGen) genOut() {
	g.pybuild.Printf("\nmod.generate(open('%v.c', 'w'))\n\n", g.cfg.Name)
	g.gofile.Printf("\n\n")
	g.genPrintOut(g.cfg.Name+".go", g.gofile)
	g.genPrintOut("build.py", g.pybuild)
	if !NoMake {
		g.makefile.Printf("\n\n")
		g.genPrintOut("Makefile", g.makefile)
	}
}

func (g *pyGen) genPkgWrapOut() {
	g.pywrap.Printf("\n\n")
	// note: must generate import string at end as imports can be added during processing
	impstr := ""
	for _, im := range g.pkg.pyimports {
		if g.mode == ModeGen || g.mode == ModeBuild {
			impstr += fmt.Sprintf("import %s\n", im)
		} else {
			impstr += fmt.Sprintf("from %s import %s\n", g.cfg.Name, im)
		}
	}
	b := g.pywrap.buf.Bytes()
	nb := bytes.Replace(b, []byte(importHereKeyString), []byte(impstr), 1)
	g.pywrap.buf = bytes.NewBuffer(nb)
	g.genPrintOut(g.pkg.pkg.Name()+".py", g.pywrap)
}

func (g *pyGen) genPkg(p *Package) {
	g.pkg = p
	g.pywrap = &printer{buf: new(bytes.Buffer), indentEach: []byte("\t")}
	g.genPyWrapPreamble()
	if p == goPackage {
		g.genGoPkg()
		g.genExtTypesPyWrap()
		g.genPkgWrapOut()
	} else {
		g.genAll()
		g.genPkgWrapOut()
	}
	g.pkg = nil
}

func (g *pyGen) genGoPreamble() {
	pkgimport := ""
	for pp, pnm := range current.imports {
		_, psfx := filepath.Split(pp)
		if psfx != pnm {
			pkgimport += fmt.Sprintf("\n\t%s %q", pnm, pp)
		} else {
			pkgimport += fmt.Sprintf("\n\t%q", pp)
		}
	}
	libcfg := func() string {
		pycfg, err := GetPythonConfig(g.cfg.VM)
		if err != nil {
			panic(err)
		}
		// this is critical to avoid pybindgen errors:
		exflags := " -Wno-error -Wno-implicit-function-declaration -Wno-int-conversion"
		pkgcfg := fmt.Sprintf(`
#cgo CFLAGS: %s
#cgo LDFLAGS: %s
`, pycfg.CFlags+exflags, pycfg.LdFlags)

		return pkgcfg
	}()

	if g.mode == ModeExe && g.cfg.Main == "" {
		g.cfg.Main = "GoPyMainRun()" // default is just to run main
	}
	exeprec := ""
	exeprego := ""
	if g.mode == ModeExe {
		exeprec = fmt.Sprintf(goExePreambleC, g.cfg.Name)
		exeprego = goExePreambleGo
	}
	g.gofile.Printf(goPreamble, g.cfg.Name, g.cfg.Cmd, libcfg, GoHandle, CGoHandle,
		pkgimport, g.cfg.Main, exeprec, exeprego)
	g.gofile.Printf("\n// --- generated code for package: %[1]s below: ---\n\n", g.cfg.Name)
}

func (g *pyGen) genPyBuildPreamble() {
	g.pybuild.Printf(PyBuildPreamble, g.cfg.Name, g.cfg.Cmd)
}

func (g *pyGen) genPyWrapPreamble() {
	n := g.pkg.pkg.Name()
	pkgimport := g.pkg.pkg.Path()
	pkgDoc := ""
	if g.pkg.doc != nil {
		pkgDoc = g.pkg.doc.Doc
	}
	if pkgDoc != "" {
		pkgDoc = `"""` + "\n" + pkgDoc + "\n" + `"""`
	}

	// import other packages for other types that we might use
	var impstr, impgenstr string
	impgenNames := []string{"_" + g.cfg.Name, "go"}
	switch {
	case g.pkg.Name() == "go":
		if g.cfg.PkgPrefix != "" {
			impgenstr += fmt.Sprintf("from %s import %s\n", g.cfg.PkgPrefix, "_"+g.cfg.Name)
		} else {
			impgenstr += fmt.Sprintf("import %s\n", "_"+g.cfg.Name)
		}
		impstr += fmt.Sprintf(GoPkgDefs, g.cfg.Name)
	case g.mode == ModeGen || g.mode == ModeBuild:
		if g.cfg.PkgPrefix != "" {
			for _, name := range impgenNames {
				impgenstr += fmt.Sprintf("from %s import %s\n", g.cfg.PkgPrefix, name)
			}
		} else {
			for _, name := range impgenNames {
				impgenstr += fmt.Sprintf("import %s\n", name)
			}
		}
	case g.mode == ModeExe:
		// exe mode ignores PkgPrefix, because it is always built in to exe
		impgenstr += fmt.Sprintf("import _%s\n", g.cfg.Name)
		impgenstr += fmt.Sprintf("from %s import go\n", g.cfg.Name)
	default:
		pkg := g.cfg.Name
		if g.cfg.PkgPrefix != "" {
			pkg = g.cfg.PkgPrefix + "." + pkg
		}
		for _, name := range impgenNames {
			impgenstr += fmt.Sprintf("from %s import %s\n", pkg, name)
		}
	}
	imps := g.pkg.pkg.Imports()
	for _, im := range imps {
		ipath := im.Path()
		if _, has := g.pkgmap[ipath]; has {
			g.pkg.AddPyImport(ipath, false)
		}
	}
	impstr += importHereKeyString

	if g.mode == ModeExe {
		g.pywrap.Printf(PyWrapExePreamble, g.cfg.Name, g.cfg.Cmd, n, pkgimport, pkgDoc, impgenstr, impstr)
	} else {
		g.pywrap.Printf(PyWrapPreamble, g.cfg.Name, g.cfg.Cmd, n, pkgimport, pkgDoc, impgenstr, impstr)
	}
}

// CmdStrToMakefile does what is needed to make the command string suitable for makefiles
// * removes -output
func CmdStrToMakefile(cmdstr string) string {
	if oidx := strings.Index(cmdstr, "-output="); oidx > 0 {
		spidx := strings.Index(cmdstr[oidx:], " ")
		cmdstr = cmdstr[:oidx] + cmdstr[oidx+spidx+1:]
	}
	cmds := strings.Fields(cmdstr)
	ncmds := make([]string, 0, len(cmds)+1)
	ncmds = append(ncmds, cmds[:2]...)
	ncmds = append(ncmds, "-no-make")
	ncmds = append(ncmds, cmds[2:]...)

	return strings.Join(ncmds, " ")
}

func (g *pyGen) genMakefile() {
	gencmd := strings.Replace(g.cfg.Cmd, "gopy build", "gopy gen", 1)
	gencmd = CmdStrToMakefile(gencmd)

	pycfg, err := GetPythonConfig(g.cfg.VM)
	if err != nil {
		panic(err)
	}

	if g.mode == ModeExe {
		g.makefile.Printf(MakefileExeTemplate, g.cfg.Name, g.cfg.Cmd, gencmd, g.cfg.VM, g.libext, pycfg.CFlags, pycfg.LdFlags)
	} else {
		winhack := ""
		if WindowsOS {
			winhack = fmt.Sprintf(`# windows-only sed hack here to fix pybindgen declaration of PyInit
  sed -i "s/ PyInit_/ __declspec(dllexport) PyInit_/g" %s.c`, g.cfg.Name)
		}
		g.makefile.Printf(MakefileTemplate, g.cfg.Name, g.cfg.Cmd, gencmd, g.cfg.VM, g.libext, g.extraGccArgs, pycfg.CFlags, pycfg.LdFlags, winhack)
	}
}

// generate external types, go code
func (g *pyGen) genExtTypesGo() {
	g.gofile.Printf("\n// ---- External Types Outside of Targeted Packages ---\n")

	names := current.names()
	for _, n := range names {
		sym := current.sym(n)
		if !sym.isType() {
			continue
		}
		if _, has := g.pkgmap[sym.gopkg.Path()]; has {
			continue
		}
		g.genType(sym, true, false) // ext types, no python wrapping
	}
}

// generate external types, py wrap
func (g *pyGen) genExtTypesPyWrap() {
	g.pywrap.Printf("\n# ---- External Types Outside of Targeted Packages ---\n")

	names := current.names()
	for _, n := range names {
		sym := current.sym(n)
		if !sym.isType() {
			continue
		}
		if _, has := g.pkgmap[sym.gopkg.Path()]; has {
			continue
		}
		g.genType(sym, true, true) // ext types, only python wrapping
	}
}

func (g *pyGen) genAll() {
	g.gofile.Printf("\n// ---- Package: %s ---\n", g.pkg.Name())

	g.gofile.Printf("\n// ---- Types ---\n")
	g.pywrap.Printf("\n# ---- Types ---\n")
	names := current.names()
	for _, n := range names {
		sym := current.sym(n)
		if sym.gopkg.Path() != g.pkg.pkg.Path() { // sometimes the package is not the same!!  yikes!
			continue
		}
		if !sym.isType() {
			continue
		}
		g.genType(sym, false, false) // not exttypes
	}

	g.pywrap.Printf("\n\n#---- Enums from Go (collections of consts with same type) ---\n")
	// conditionally add Enum support because it is an external dependency in py2
	if len(g.pkg.enums) > 0 {
		g.pywrap.Printf("from enum import Enum\n\n")
	}
	for _, e := range g.pkg.enums {
		g.genEnum(e)
	}

	g.pywrap.Printf("\n\n#---- Constants from Go: Python can only ask that you please don't change these! ---\n")
	for _, c := range g.pkg.consts {
		g.genConst(c)
	}

	g.gofile.Printf("\n\n// ---- Global Variables: can only use functions to access ---\n")
	g.pywrap.Printf("\n\n# ---- Global Variables: can only use functions to access ---\n")
	for _, v := range g.pkg.vars {
		g.genVar(v)
	}

	g.gofile.Printf("\n\n// ---- Interfaces ---\n")
	g.pywrap.Printf("\n\n# ---- Interfaces ---\n")
	for _, ifc := range g.pkg.ifaces {
		g.genInterface(ifc)
	}

	g.gofile.Printf("\n\n// ---- Structs ---\n")
	g.pywrap.Printf("\n\n# ---- Structs ---\n")
	g.pkg.sortStructEmbeds()
	for _, s := range g.pkg.structs {
		g.genStruct(s)
	}

	g.gofile.Printf("\n\n// ---- Slices ---\n")
	g.pywrap.Printf("\n\n# ---- Slices ---\n")
	for _, s := range g.pkg.slices {
		g.genSlice(s.sym, false, false, s)
	}

	g.gofile.Printf("\n\n// ---- Maps ---\n")
	g.pywrap.Printf("\n\n# ---- Maps ---\n")
	for _, m := range g.pkg.maps {
		g.genMap(m.sym, false, false, m)
	}

	// note: these are extracted from reg functions that return full
	// type (not pointer -- should do pointer but didn't work yet)
	g.gofile.Printf("\n\n// ---- Constructors ---\n")
	g.pywrap.Printf("\n\n# ---- Constructors ---\n")
	for _, s := range g.pkg.structs {
		for _, ctor := range s.ctors {
			g.genFunc(ctor)
		}
	}

	g.gofile.Printf("\n\n// ---- Functions ---\n")
	g.pywrap.Printf("\n\n# ---- Functions ---\n")
	for _, f := range g.pkg.funcs {
		g.genFunc(f)
	}
}

func (g *pyGen) genGoPkg() {
	g.gofile.Printf("\n// ---- Package: %s ---\n", g.pkg.Name())

	g.gofile.Printf("\n// ---- Types ---\n")
	g.pywrap.Printf("\n# ---- Types ---\n")
	names := universe.names()
	for _, n := range names {
		sym := universe.sym(n)
		if sym.gopkg == nil && sym.goname == "interface{}" {
			g.genType(sym, false, false)
			continue
		}
		if sym.gopkg == nil {
			continue
		}
		if !sym.isType() || sym.gopkg.Path() != g.pkg.pkg.Path() {
			continue
		}
		g.genType(sym, false, false) // not exttypes
	}
}
