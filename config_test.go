package main

import (
	"testing"
)

func TestConfig(t *testing.T) {
	conf, err := NewConfig("app.yaml")
	if err != nil {
		t.Fatal(err)
		return
	}
	t.Log("conf is ", conf)
}
