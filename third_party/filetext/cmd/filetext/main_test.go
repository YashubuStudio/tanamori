package main

import (
	"reflect"
	"testing"
)

func TestInterspersedArgs(t *testing.T) {
	got := interspersedArgs([]string{"first.txt", "--lines", "2-3", "second.pdf", "--ignore-case"})
	want := []string{"--lines", "2-3", "--ignore-case", "first.txt", "second.pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
func TestInterspersedArgsKeepsFormatValueAndDetectFlag(t *testing.T) {
	got := interspersedArgs([]string{"payload.data", "--format", "json", "--detect"})
	want := []string{"--format", "json", "--detect", "payload.data"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
