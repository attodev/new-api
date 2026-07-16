package controller

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const tossPaymentEventResolutionNoteMaxRunes = 2000

type tossPaymentEventDetailResponse struct {
	*model.TossPaymentEvent
	ProviderPayload string `json:"provider_payload"`
}

type resolveTossPaymentEventRequest struct {
	ResolutionNote string `json:"resolution_note"`
}

func tossPaymentEventDetail(event *model.TossPaymentEvent) *tossPaymentEventDetailResponse {
	if event == nil {
		return nil
	}
	return &tossPaymentEventDetailResponse{
		TossPaymentEvent: event,
		ProviderPayload:  event.ProviderPayload,
	}
}

func isValidTossPaymentEventType(eventType string) bool {
	switch eventType {
	case model.TossPaymentEventTypeCancellation,
		model.TossPaymentEventTypeFulfillment,
		model.TossPaymentEventTypeFinancialMismatch,
		model.TossPaymentEventTypeRefundRequired:
		return true
	default:
		return false
	}
}

func ListTossPaymentReconciliationEvents(c *gin.Context) {
	status := strings.TrimSpace(c.Query("status"))
	if status == "" {
		status = model.TossReconciliationStatusRequired
	} else if status == "all" {
		status = ""
	} else if status != model.TossReconciliationStatusRequired && status != model.TossReconciliationStatusResolved {
		common.ApiError(c, errors.New("invalid Toss reconciliation status"))
		return
	}
	eventType := strings.TrimSpace(c.Query("event_type"))
	if eventType != "" && !isValidTossPaymentEventType(eventType) {
		common.ApiError(c, errors.New("invalid Toss payment event type"))
		return
	}
	orderId := strings.TrimSpace(c.Query("order_id"))
	if len(orderId) > 255 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	pageInfo := common.GetPageQuery(c)
	events, total, err := model.ListTossPaymentEvents(model.TossPaymentEventFilter{
		ReconciliationStatus: status,
		EventType:            eventType,
		OrderId:              orderId,
	}, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(events)
	common.ApiSuccess(c, pageInfo)
}

func GetTossPaymentReconciliationEvent(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	event, err := model.GetTossPaymentEventById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, tossPaymentEventDetail(event))
}

func ResolveTossPaymentReconciliationEvent(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var req resolveTossPaymentEventRequest
	if err := decodeTossPaymentRequestJSON(c, &req); err != nil {
		if common.IsRequestBodyTooLargeError(err) {
			respondTossPaymentRequestBodyTooLarge(c)
			return
		}
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	req.ResolutionNote = strings.TrimSpace(req.ResolutionNote)
	if req.ResolutionNote == "" || len([]rune(req.ResolutionNote)) > tossPaymentEventResolutionNoteMaxRunes {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	event, err := model.ResolveTossPaymentEventByAdmin(id, c.GetInt("id"), req.ResolutionNote)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, tossPaymentEventDetail(event))
}
