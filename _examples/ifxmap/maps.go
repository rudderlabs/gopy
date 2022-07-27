// Copyright 2015 The go-python Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ifxmap

type TestStruct struct {
	Value int
}

func New() map[string]interface{} {
	retval := make(map[string]interface{})

	retval["float"] = 3.14159
	retval["string"] = "sample"
	retval["int"] = 2
	retval["uint"] = uint64(3)
	retval["bool"] = true
	retval["struct"] = TestStruct{
		Value: 6,
	}

	structPtr := TestStruct{
		Value: 12,
	}
	retval["map"] = map[string]interface{}{
		"string": "abcd",
		"int":    1,
		"float":  2.69,
	}
	retval["structPtr"] = &structPtr

	return retval
}
