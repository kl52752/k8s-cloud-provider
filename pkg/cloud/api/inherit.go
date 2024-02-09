/*
Copyright 2024 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"fmt"
	"reflect"

	"k8s.io/klog/v2"
)

func Inherit[T any](to, from T, trait *FieldTraits) error {
	paths := pathsToInherited(trait)
	if len(paths) == 0 {
		klog.Infof("No paths to inherit")
		return nil
	}
	return inherit(reflect.ValueOf(to), reflect.ValueOf(from), paths)
}

func pathsToInherited(traits *FieldTraits) []Path {
	paths := []Path{}
	for _, f := range traits.fields {
		if f.fType == FieldTypeInherited {
			paths = append(paths, f.path)
		}
	}
	return paths
}

// inherit copies values defines in paths from object `from` into object `to`
func inherit(to, from reflect.Value, paths []Path) error {
	for _, path := range paths {
		inheritPath(path, to, from)
	}

	return nil
}

func inheritPath(p Path, to, from reflect.Value) error {
	klog.Infof("Print Path %s", p)
	gotVal, err := p.ResolveValue(from)
	if err != nil {
		return err
	}

	err = setValue(p, to, gotVal)
	if err != nil {
		klog.Errorf("setValue(p, to, gotVal) = %v", err)
	}
	return err
}

func setValue(p Path, to, from reflect.Value) error {
	// we need to traverse whole path to check if there are some non initialized
	// values there
	klog.Infof("start set value %v for: %s", from, p)
	for i, pi := range p {
		if !to.IsValid() {
			return fmt.Errorf("element is invalid: %s", p[0:i])
		}
		if to.IsZero() {
			klog.Infof("element is zero: %s", p[0:i])
		}
		switch pi[0] {
		case pathField:
			if to.Kind() != reflect.Struct {
				return fmt.Errorf("at %s, expected struct, got %v", p[0:i], to.Kind())
			}

			fieldName := pi[1:]
			to = to.FieldByName(fieldName)
		case pathSliceIndex, pathMapIndex:
			return fmt.Errorf("unsupported path type %q", pi[0])
		case pathPointer:
			if to.IsZero() {
				klog.Infof("element is zero: %s", p[0:i])
				// TODO(kl52752) Skip pointers right now we need to create
				// object if it is nil
				continue
			}
			if to.Kind() != reflect.Pointer {
				return fmt.Errorf("at %v, expected pointer, got %v", p[0:i], to.Kind())
			}
			to = to.Elem()
		default:
			return fmt.Errorf("at %s, invalid path type %q", p[0:i], pi[0])
		}
	}
	klog.Infof("After check %+v", to)
	// TODO(kl52752) right now we set only basic type, this can be extended to
	// setting whole structs.
	if isBasicV(to) {
		to.Set(from)
		klog.Infof("After set %+v", to)
	}
	return nil
}
