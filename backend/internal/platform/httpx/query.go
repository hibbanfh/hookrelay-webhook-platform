package httpx

import "net/http"

// OptionalQueryValue reads a query parameter that may be absent. It reports whether the
// parameter was sent at all, so an empty value (?status=) is told apart from no parameter. A
// parameter sent more than once is a 400 on that parameter instead of silently using the first.
func OptionalQueryValue(r *http.Request, name string) (value string, present bool, err error) {
	values, present := r.URL.Query()[name]
	if !present {
		return "", false, nil
	}
	if len(values) > 1 {
		return "", false, invalidField(name, name+" must be sent at most once")
	}
	return values[0], true, nil
}

// LimitFromQuery reads the limit parameter like ParseLimit, and rejects one sent more than once.
func LimitFromQuery(r *http.Request) (int, error) {
	raw, _, err := OptionalQueryValue(r, "limit")
	if err != nil {
		return 0, err
	}
	return ParseLimit(raw)
}

// CursorFromQuery returns the cursor parameter, empty when absent, and rejects one sent more than once.
func CursorFromQuery(r *http.Request) (string, error) {
	raw, _, err := OptionalQueryValue(r, "cursor")
	return raw, err
}
