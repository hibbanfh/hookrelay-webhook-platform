package usecase_test

import "context"

// fakeURLs is a repository.EndpointURLLister over a fixed set.
type fakeURLs struct {
	rec  *recorder
	urls map[string]struct{}
	err  error
}

func (f *fakeURLs) ListURLs(context.Context, string) (map[string]struct{}, error) {
	f.rec.add("urls")
	return f.urls, f.err
}
