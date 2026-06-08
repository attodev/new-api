package model

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

type OrganizationDashboard struct {
	Organization OrganizationDashboardOrganization      `json:"organization"`
	Summary      OrganizationDashboardSummary           `json:"summary"`
	DailyUsage   []OrganizationDashboardDailyUsage      `json:"daily_usage"`
	TopUsers     []OrganizationDashboardUserUsage       `json:"top_users"`
	TopModels    []OrganizationDashboardModelUsage      `json:"top_models"`
	ModelUsage   []OrganizationDashboardModelDailyUsage `json:"model_usage"`
	UserUsage    []OrganizationDashboardUserDailyUsage  `json:"user_usage"`
}

type OrganizationDashboardOrganization struct {
	Id        int    `json:"id"`
	Name      string `json:"name"`
	Quota     int    `json:"quota"`
	UsedQuota int    `json:"used_quota"`
}

type OrganizationDashboardSummary struct {
	PeriodQuota       int `json:"period_quota"`
	PeriodRequests    int `json:"period_requests"`
	MemberCount       int `json:"member_count"`
	ActiveMemberCount int `json:"active_member_count"`
}

type OrganizationDashboardDailyUsage struct {
	Date     string `json:"date"`
	Quota    int    `json:"quota"`
	Requests int    `json:"requests"`
}

type OrganizationDashboardUserUsage struct {
	UserID         int    `json:"user_id"`
	Username       string `json:"username"`
	DisplayName    string `json:"display_name"`
	Quota          int    `json:"quota"`
	UsedQuota      int    `json:"used_quota"`
	PeriodQuota    int    `json:"period_quota"`
	PeriodRequests int    `json:"period_requests"`
}

type OrganizationDashboardUserDailyUsage struct {
	Date        string `json:"date"`
	UserID      int    `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Quota       int    `json:"quota"`
	Requests    int    `json:"requests"`
}

type OrganizationDashboardModelUsage struct {
	Model    string `json:"model"`
	Quota    int    `json:"quota"`
	Requests int    `json:"requests"`
}

type OrganizationDashboardModelDailyUsage struct {
	Date     string `json:"date"`
	Model    string `json:"model"`
	Quota    int    `json:"quota"`
	Requests int    `json:"requests"`
}

func GetOrganizationDashboard(organizationId int, startTime int64, endTime int64) (*OrganizationDashboard, error) {
	startTime, endTime = normalizeOrganizationDashboardTimeRange(startTime, endTime)

	var organization Organization
	if err := DB.First(&organization, organizationId).Error; err != nil {
		return nil, err
	}

	dashboard := &OrganizationDashboard{
		Organization: OrganizationDashboardOrganization{
			Id:        organization.Id,
			Name:      organization.Name,
			Quota:     organization.Quota,
			UsedQuota: organization.UsedQuota,
		},
		DailyUsage: []OrganizationDashboardDailyUsage{},
		TopUsers:   []OrganizationDashboardUserUsage{},
		TopModels:  []OrganizationDashboardModelUsage{},
		ModelUsage: []OrganizationDashboardModelDailyUsage{},
		UserUsage:  []OrganizationDashboardUserDailyUsage{},
	}

	var users []User
	if err := DB.Where("organization_id = ?", organizationId).Find(&users).Error; err != nil {
		return nil, err
	}
	dashboard.Summary.MemberCount = len(users)
	if len(users) == 0 {
		return dashboard, nil
	}

	userIDs := make([]int, 0, len(users))
	usersByID := make(map[int]User, len(users))
	for _, user := range users {
		userIDs = append(userIDs, user.Id)
		usersByID[user.Id] = user
	}

	var quotaRows []QuotaData
	if err := DB.
		Where("user_id IN ? AND created_at >= ? AND created_at <= ?", userIDs, startTime, endTime).
		Find(&quotaRows).Error; err != nil {
		return nil, err
	}

	dailyUsage := map[string]*OrganizationDashboardDailyUsage{}
	userUsage := map[int]*OrganizationDashboardUserUsage{}
	userDailyUsage := map[string]*OrganizationDashboardUserDailyUsage{}
	modelUsage := map[string]*OrganizationDashboardModelUsage{}
	modelDailyUsage := map[string]*OrganizationDashboardModelDailyUsage{}
	activeUsers := map[int]bool{}

	for _, quotaRow := range quotaRows {
		dashboard.Summary.PeriodQuota += quotaRow.Quota
		dashboard.Summary.PeriodRequests += quotaRow.Count
		activeUsers[quotaRow.UserID] = true

		date := time.Unix(quotaRow.CreatedAt, 0).UTC().Format("2006-01-02")
		if dailyUsage[date] == nil {
			dailyUsage[date] = &OrganizationDashboardDailyUsage{Date: date}
		}
		dailyUsage[date].Quota += quotaRow.Quota
		dailyUsage[date].Requests += quotaRow.Count

		if userUsage[quotaRow.UserID] == nil {
			user := usersByID[quotaRow.UserID]
			displayName := user.DisplayName
			if displayName == "" {
				displayName = user.Username
			}
			userUsage[quotaRow.UserID] = &OrganizationDashboardUserUsage{
				UserID:      quotaRow.UserID,
				Username:    user.Username,
				DisplayName: displayName,
				Quota:       user.Quota,
				UsedQuota:   user.UsedQuota,
			}
		}
		userUsage[quotaRow.UserID].PeriodQuota += quotaRow.Quota
		userUsage[quotaRow.UserID].PeriodRequests += quotaRow.Count

		userDailyUsageKey := date + "\x00" + strconv.Itoa(quotaRow.UserID)
		if userDailyUsage[userDailyUsageKey] == nil {
			user := usersByID[quotaRow.UserID]
			displayName := user.DisplayName
			if displayName == "" {
				displayName = user.Username
			}
			userDailyUsage[userDailyUsageKey] = &OrganizationDashboardUserDailyUsage{
				Date:        date,
				UserID:      quotaRow.UserID,
				Username:    user.Username,
				DisplayName: displayName,
			}
		}
		userDailyUsage[userDailyUsageKey].Quota += quotaRow.Quota
		userDailyUsage[userDailyUsageKey].Requests += quotaRow.Count

		modelName := strings.TrimSpace(quotaRow.ModelName)
		if modelName == "" {
			modelName = "(unknown)"
		}
		if modelUsage[modelName] == nil {
			modelUsage[modelName] = &OrganizationDashboardModelUsage{Model: modelName}
		}
		modelUsage[modelName].Quota += quotaRow.Quota
		modelUsage[modelName].Requests += quotaRow.Count

		modelDailyUsageKey := date + "\x00" + modelName
		if modelDailyUsage[modelDailyUsageKey] == nil {
			modelDailyUsage[modelDailyUsageKey] = &OrganizationDashboardModelDailyUsage{
				Date:  date,
				Model: modelName,
			}
		}
		modelDailyUsage[modelDailyUsageKey].Quota += quotaRow.Quota
		modelDailyUsage[modelDailyUsageKey].Requests += quotaRow.Count
	}
	dashboard.Summary.ActiveMemberCount = len(activeUsers)

	for _, usage := range dailyUsage {
		dashboard.DailyUsage = append(dashboard.DailyUsage, *usage)
	}
	sort.Slice(dashboard.DailyUsage, func(i, j int) bool {
		return dashboard.DailyUsage[i].Date < dashboard.DailyUsage[j].Date
	})

	for _, usage := range userUsage {
		dashboard.TopUsers = append(dashboard.TopUsers, *usage)
	}
	sort.Slice(dashboard.TopUsers, func(i, j int) bool {
		if dashboard.TopUsers[i].PeriodQuota == dashboard.TopUsers[j].PeriodQuota {
			return dashboard.TopUsers[i].Username < dashboard.TopUsers[j].Username
		}
		return dashboard.TopUsers[i].PeriodQuota > dashboard.TopUsers[j].PeriodQuota
	})

	for _, usage := range userDailyUsage {
		dashboard.UserUsage = append(dashboard.UserUsage, *usage)
	}
	sort.Slice(dashboard.UserUsage, func(i, j int) bool {
		if dashboard.UserUsage[i].Date == dashboard.UserUsage[j].Date {
			return dashboard.UserUsage[i].Username < dashboard.UserUsage[j].Username
		}
		return dashboard.UserUsage[i].Date < dashboard.UserUsage[j].Date
	})

	for _, usage := range modelUsage {
		dashboard.TopModels = append(dashboard.TopModels, *usage)
	}
	sort.Slice(dashboard.TopModels, func(i, j int) bool {
		if dashboard.TopModels[i].Quota == dashboard.TopModels[j].Quota {
			return dashboard.TopModels[i].Model < dashboard.TopModels[j].Model
		}
		return dashboard.TopModels[i].Quota > dashboard.TopModels[j].Quota
	})

	for _, usage := range modelDailyUsage {
		dashboard.ModelUsage = append(dashboard.ModelUsage, *usage)
	}
	sort.Slice(dashboard.ModelUsage, func(i, j int) bool {
		if dashboard.ModelUsage[i].Date == dashboard.ModelUsage[j].Date {
			return dashboard.ModelUsage[i].Model < dashboard.ModelUsage[j].Model
		}
		return dashboard.ModelUsage[i].Date < dashboard.ModelUsage[j].Date
	})

	return dashboard, nil
}

func normalizeOrganizationDashboardTimeRange(startTime int64, endTime int64) (int64, int64) {
	if startTime > 0 && endTime > 0 && startTime <= endTime {
		return startTime, endTime
	}

	endTime = time.Now().Unix()
	startTime = endTime - int64(7*24*time.Hour/time.Second)
	return startTime, endTime
}
