package main

import (
	"slices"
	"testing"

	endpointuc "hookrelay/internal/endpoint/domain/usecase"
)

func TestDemoEndpointSpecs(t *testing.T) {
	const project = "proj_01JC3ZK6Q8V2W7T4N5R9XBYH0M"
	tests := []struct {
		name    string
		baseURL string
		want    []endpointuc.DemoEndpointSpec
	}{
		{
			name: "https base", baseURL: "https://hr.example/demo/receiver",
			want: []endpointuc.DemoEndpointSpec{
				{URL: "https://hr.example/demo/receiver/" + project, Description: "Demo receiver (healthy)"},
				{URL: "https://hr.example/demo/receiver/" + project + "?fail_first=2", Description: "Demo receiver (flaky: fails twice)"},
			},
		},
		{
			name: "private compose base", baseURL: "http://api:8080/demo/receiver",
			want: []endpointuc.DemoEndpointSpec{
				{URL: "http://api:8080/demo/receiver/" + project, Description: "Demo receiver (healthy)"},
				{URL: "http://api:8080/demo/receiver/" + project + "?fail_first=2", Description: "Demo receiver (flaky: fails twice)"},
			},
		},
		{
			name: "a trailing slash is not doubled", baseURL: "http://api:8080/demo/receiver/",
			want: []endpointuc.DemoEndpointSpec{
				{URL: "http://api:8080/demo/receiver/" + project, Description: "Demo receiver (healthy)"},
				{URL: "http://api:8080/demo/receiver/" + project + "?fail_first=2", Description: "Demo receiver (flaky: fails twice)"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := demoEndpointSpecs(tt.baseURL, project)

			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
