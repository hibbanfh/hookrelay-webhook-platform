package httppresenter_test

import (
	"net/http"
	"testing"

	"hookrelay/internal/platform/config"
	"hookrelay/internal/project/domain/entity"
	"hookrelay/internal/project/domain/usecase"
)

const demoBase = "https://hr.example/demo/receiver"

// demoURLCase is one route that shows a project, asked with and without a demo receiver base URL.
type demoURLCase struct {
	name       string
	method     string
	path       string
	body       string
	wantStatus int
	// prepare gets the API (before it is built) and returns the headers of the request.
	prepare func(t *testing.T, a *sessAPI) map[string]string
}

func demoURLCases() []demoURLCase {
	return []demoURLCase{
		{
			name: "GET /v1/project", method: http.MethodGet, path: "/v1/project", wantStatus: http.StatusOK,
			prepare: func(*testing.T, *sessAPI) map[string]string { return session("tok-A") },
		},
		{
			name: "POST /v1/projects", method: http.MethodPost, path: "/v1/projects", body: `{"name":"Acme"}`, wantStatus: http.StatusCreated,
			prepare: func(*testing.T, *sessAPI) map[string]string { return nil },
		},
		{
			name: "POST /v1/me/project creating", method: http.MethodPost, path: "/v1/me/project", wantStatus: http.StatusCreated,
			prepare: func(t *testing.T, a *sessAPI) map[string]string {
				a.ensurer.out = usecase.EnsureUserProjectOutput{
					Project: entity.Project{ID: "proj_N", Name: "N", CreatedAt: fixedNow, OwnerID: userA}, APIKey: mustRawKey(t), Created: true,
				}
				return session("tok-A")
			},
		},
		{
			name: "POST /v1/me/project existing", method: http.MethodPost, path: "/v1/me/project", wantStatus: http.StatusOK,
			prepare: func(t *testing.T, a *sessAPI) map[string]string {
				a.ensurer.out = usecase.EnsureUserProjectOutput{Project: entity.Project{ID: "proj_N", Name: "N", CreatedAt: fixedNow}}
				return session("tok-A")
			},
		},
	}
}

func TestEveryProjectRouteShowsTheDemoReceiverURLWhenEnabled(t *testing.T) {
	for _, tt := range demoURLCases() {
		t.Run(tt.name, func(t *testing.T) {
			a := newSessionAPI(t)
			a.demoBaseURL = demoBase
			headers := tt.prepare(t, a)
			a.handler = a.build()

			rec := do(a.handler, tt.method, tt.path, tt.body, headers)

			body := decodeMap(t, rec)
			id, _ := body["id"].(string)
			// config.DemoReceiverURL is the one builder of this URL; the presenter cannot import config,
			// so this assertion pins its own copy to it.
			if rec.Code != tt.wantStatus || id == "" || body["demo_receiver_url"] != config.DemoReceiverURL(demoBase, id) {
				t.Fatalf("got %d %s; want demo_receiver_url %s/%s", rec.Code, rec.Body.String(), demoBase, id)
			}
		})
	}
}

func TestEveryProjectRouteShowsANullDemoReceiverURLWhenDisabled(t *testing.T) {
	for _, tt := range demoURLCases() {
		t.Run(tt.name, func(t *testing.T) {
			a := newSessionAPI(t)
			headers := tt.prepare(t, a)
			a.handler = a.build()

			rec := do(a.handler, tt.method, tt.path, tt.body, headers)

			body := decodeMap(t, rec)
			url, present := body["demo_receiver_url"]
			if rec.Code != tt.wantStatus || !present || url != nil {
				t.Fatalf("got %d %s; want demo_receiver_url present and null", rec.Code, rec.Body.String())
			}
		})
	}
}
