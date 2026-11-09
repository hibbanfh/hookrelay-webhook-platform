package httppresenter

import (
	"strings"

	"hookrelay/internal/project/delivery/dto"
	"hookrelay/internal/project/domain/entity"
	"hookrelay/internal/project/domain/usecase"
)

func toCreateProjectInput(r dto.CreateProjectRequest) usecase.CreateProjectInput {
	return usecase.CreateProjectInput{Name: r.Name}
}

// toProjectResponse maps an entity to its DTO. demoBaseURL is empty when the demo
// receiver is disabled, which yields a null demo_receiver_url.
func toProjectResponse(p entity.Project, demoBaseURL string) dto.ProjectResponse {
	return dto.ProjectResponse{
		ID:              p.ID,
		Name:            p.Name,
		CreatedAt:       p.CreatedAt.UTC(),
		DemoReceiverURL: demoReceiverURL(demoBaseURL, p.ID),
	}
}

func toProjectCreatedResponse(out usecase.CreateProjectOutput, demoBaseURL string) dto.ProjectCreatedResponse {
	return toCreatedResponse(out.Project, out.APIKey, demoBaseURL)
}

// toCreatedResponse is the only mapping that reveals a plaintext key.
func toCreatedResponse(p entity.Project, key entity.RawKey, demoBaseURL string) dto.ProjectCreatedResponse {
	return dto.ProjectCreatedResponse{
		ProjectResponse: toProjectResponse(p, demoBaseURL),
		APIKey:          key.Reveal(),
	}
}

func demoReceiverURL(baseURL, projectID string) *string {
	if baseURL == "" {
		return nil
	}
	u := strings.TrimRight(baseURL, "/") + "/" + projectID
	return &u
}
