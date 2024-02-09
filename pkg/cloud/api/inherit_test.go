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
	"reflect"
	"testing"
)

func structTraits() *FieldTraits {
	return &FieldTraits{
		fields: []fieldTrait{
			{
				path:  Path{}.Pointer().Field("I"),
				fType: FieldTypeInherited,
			},
			{
				path:  Path{}.Pointer().Field("Sta").Field("Sb").Field("C"),
				fType: FieldTypeInherited,
			},
		},
	}
}

func TestInheritance(t *testing.T) {
	type StB struct {
		C int
	}
	type StA struct {
		B  string
		C  int
		Sb StB
	}
	type St struct {
		I    int
		PS   *string
		LSta []StB
		Sta  StA
		PSta *StA
	}

	s := "some string"
	s1 := St{}
	s2 := St{
		I:    1,
		PS:   &s,
		LSta: []StB{{C: 1}, {C: 2}},
		Sta: StA{
			B:  "b",
			C:  7,
			Sb: StB{C: 5},
		},
		PSta: &StA{
			B:  "p",
			C:  8,
			Sb: StB{C: 5},
		},
	}
	for _, tc := range []struct {
		name    string
		to      reflect.Value
		from    reflect.Value
		trait   *FieldTraits
		wantErr bool
	}{{
		name:  "pointer",
		to:    reflect.ValueOf(&s1),
		from:  reflect.ValueOf(&s2),
		trait: structTraits(),
	},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Inherit(tc.to, tc.from, tc.trait)
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Fatalf("CheckStructuralSubset() = %v; gotErr = %t, want %t", err, gotErr, tc.wantErr)
			}
			t.Logf("After inherit: %+v", tc.to)
		})
	}
}
