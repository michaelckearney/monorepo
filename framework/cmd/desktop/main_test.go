package main

import (
	"reflect"
	"testing"
)

func TestSelfSteps(t *testing.T) {
	vet, test := []string{"vet", "./..."}, []string{"test", "./..."}
	for _, c := range []struct {
		args  []string
		steps [][]string
		full  bool
	}{
		{nil, [][]string{vet, test}, true},
		{[]string{"-e2e"}, nil, false},
		{[]string{"-lint"}, [][]string{vet}, false},
		{[]string{"-lint", "-unit"}, [][]string{vet, test}, false},
		{[]string{"-short"}, [][]string{vet, {"test", "-short", "./..."}}, false},
		{[]string{"-unit", "-run", "TestX"}, [][]string{{"test", "-run", "TestX", "./..."}}, false},
	} {
		steps, full := selfSteps(c.args)
		if !reflect.DeepEqual(steps, c.steps) || full != c.full {
			t.Errorf("selfSteps(%q) = %q, %v; want %q, %v", c.args, steps, full, c.steps, c.full)
		}
	}
}
