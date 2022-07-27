package convertor

/*

#cgo CFLAGS: -I/Library/Developer/CommandLineTools/Library/Frameworks/Python3.framework/Versions/3.8/include/python3.8 -Wno-error -Wno-implicit-function-declaration -Wno-int-conversion
#cgo LDFLAGS: -L/Applications/Xcode.app/Contents/Developer/Library/Frameworks/Python3.framework/Versions/3.8/lib -lpython3.8 -ldl -lSystem  -framework CoreFoundation

// #define Py_LIMITED_API // need full API for PyRun*
#include <Python.h>
typedef uint8_t bool;
// static inline is trick for avoiding need for extra .c file
// the following are used for build value -- switch on reflect.Kind
// or the types equivalent
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
static PyObject* Py_BuildValue2(char *format, long long arg0)
{
	PyObject *retval = Py_BuildValue(format, arg0);
	free(format);
	return retval;
}
static PyObject*
Py_BuildGenericStruct(char *objType, long long handle)
{
    PyObject *hello_module = PyImport_ImportModule("out.ifxmap");
    PyObject *testStructCls = PyObject_GetAttrString(hello_module, objType);
	PyObject *argTuple = PyTuple_New(1);
	PyObject *handlePO = Py_BuildValue("L", handle);
	PyTuple_SetItem(argTuple, 0, handlePO);

    PyObject* result = PyObject_CallObject(testStructCls, argTuple);

    Py_DECREF(testStructCls);
    Py_DECREF(hello_module);

    return result;
}
static PyObject*
Build_Map_string_interface(long long handle)
{
    PyObject *hello_module = PyImport_ImportModule("out.ifxmap");
    PyObject *testStructCls = PyObject_GetAttrString(hello_module, "Map_string_interface_");
	PyObject *argTuple = PyTuple_New(0);
	//PyTuple_SetItem(argTuple, 0, handle);
	PyObject *handlePO = Py_BuildValue("L", handle);
	PyObject *kwargs = PyDict_New();
	PyDict_SetItem(kwargs, gopy_build_string("handle"), handlePO);

    PyObject* result = PyObject_Call(testStructCls, argTuple, kwargs);

    Py_DECREF(testStructCls);
    Py_DECREF(hello_module);

    return result;
}
*/
import "C"
import (
	"fmt"
	"github.com/go-python/gopy/gopyh"
	"reflect"
	"strings"
)

type CGoHandle C.longlong

func handleFromPtrGenericStruct(p interface{}, structType string) CGoHandle {
	return CGoHandle(gopyh.Register(structType, p))
}

func Convert(arg interface{}) *C.PyObject {
	switch reflect.ValueOf(arg).Kind() {
	case reflect.Int:
		x := arg.(int)
		return C.gopy_build_int64(C.longlong(x))
	case reflect.Uint64:
		x := arg.(uint64)
		return C.gopy_build_uint64(C.ulonglong(x))
	case reflect.Bool:
		x := arg.(bool)
		if x {
			return C.Py_True
		}
		return C.Py_False
	case reflect.Float64:
		x := arg.(float64)
		return C.gopy_build_float64(C.double(x))
	case reflect.String:
		x := arg.(string)
		return C.gopy_build_string(C.CString(x))
	case reflect.Interface:
		return Convert(reflect.ValueOf(arg))
	case reflect.Struct:
		objType := fmt.Sprintf("%T", arg)
		y := handleFromPtrGenericStruct(&arg, objType)
		pyObjectTypes := strings.Split(objType, ".")
		return C.Py_BuildGenericStruct(C.CString(pyObjectTypes[len(pyObjectTypes)-1]), C.longlong(y))
	case reflect.Map:
		objType := fmt.Sprintf("%T", arg)
		x, ok := arg.(map[string]interface{})
		if !ok {
			e := "Invalid type: " + reflect.ValueOf(arg).Kind().String() + " value:" + reflect.ValueOf(arg).String()
			return C.gopy_build_string(C.CString(e))
		}
		y := handleFromPtrGenericStruct(&x, objType)
		return C.Build_Map_string_interface(C.longlong(y))
	case reflect.Ptr:
		objType := fmt.Sprintf("%T", arg)
		y := handleFromPtrGenericStruct(arg, objType)
		pyObjectTypes := strings.Split(objType, ".")
		return C.Py_BuildGenericStruct(C.CString(pyObjectTypes[len(pyObjectTypes)-1]), C.longlong(y))
	}
	e := reflect.ValueOf(arg).Kind().String() + reflect.ValueOf(arg).String()
	x := C.CString(e)
	return C.gopy_build_string(x)
}
