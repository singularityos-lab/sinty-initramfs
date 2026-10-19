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

func TestTrialPending(t *testing.T) {
	cases := []struct {
		state string
		want  bool
	}{
		{"target=next\ntrial=1\nattempts=2\n", true},
		{"target=next\ntrial=1\nattempts=0\n", true},
		{"target=active\ntrial=0\nattempts=0\n", false},
		{"target=next\ntrial=0\nattempts=0\n", false},
		{"target=recovery\n", false},
		{"", false},
	}
	for _, c := range cases {
		if got := trialPending(c.state); got != c.want {
			t.Errorf("trialPending(%q) = %v, want %v", c.state, got, c.want)
		}
	}
}
