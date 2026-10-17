package main

import (
	"reflect"
	"testing"
)

func TestInitArgs(t *testing.T) {
	cases := []struct {
		init  string
		debug bool
		want  []string
	}{
		{"/usr/lib/sinit/sinit", false, nil},
		{"/usr/lib/sinit/sinit", true, []string{"init", "--debug"}},
		{"/sbin/init", true, []string{"--debug"}},
	}
	for _, c := range cases {
		if got := initArgs(c.init, c.debug); !reflect.DeepEqual(got, c.want) {
			t.Errorf("initArgs(%q, %v) = %v, want %v", c.init, c.debug, got, c.want)
		}
	}
}
