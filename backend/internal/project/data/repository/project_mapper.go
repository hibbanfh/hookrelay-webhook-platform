package repositoryimpl

import (
	"hookrelay/internal/project/data/model"
	"hookrelay/internal/project/domain/entity"
)

// toProjectModel maps "" (no owner) to a NULL owner column.
func toProjectModel(p entity.Project) model.Project {
	m := model.Project{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt}
	if p.OwnerID != "" {
		m.NeonUserID = &p.OwnerID
	}
	return m
}

func toProjectEntity(m model.Project) entity.Project {
	p := entity.Project{ID: m.ID, Name: m.Name, CreatedAt: m.CreatedAt.UTC()}
	if m.NeonUserID != nil {
		p.OwnerID = *m.NeonUserID
	}
	return p
}

func toAPIKeyModel(k entity.APIKey) model.APIKey {
	return model.APIKey{
		ID:        k.ID,
		ProjectID: k.ProjectID,
		Prefix:    k.Prefix,
		Hash:      k.Hash[:],
		CreatedAt: k.CreatedAt,
		RevokedAt: k.RevokedAt,
	}
}
