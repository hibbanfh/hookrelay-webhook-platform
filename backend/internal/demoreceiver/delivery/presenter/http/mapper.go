package httppresenter

import (
	"hookrelay/internal/demoreceiver/delivery/dto"
	"hookrelay/internal/demoreceiver/domain/entity"
)

func toResultResponse(r entity.Receipt) dto.DemoReceiverResultResponse {
	return dto.DemoReceiverResultResponse{
		Received: true, EndpointID: r.EndpointID, WebhookID: r.WebhookID, RequestNumber: r.RequestNumber, FailFirst: r.FailFirst,
	}
}
