/*
 * Copyright (c) 2020 Devtron Labs
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package restHandler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	application2 "github.com/argoproj/argo-cd/pkg/apiclient/application"
	"github.com/argoproj/argo-cd/pkg/apis/application/v1alpha1"
	"github.com/devtron-labs/devtron/api/bean"
	"github.com/devtron-labs/devtron/client/argocdServer/application"
	"github.com/devtron-labs/devtron/internal/constants"
	"github.com/devtron-labs/devtron/internal/util"
	"github.com/devtron-labs/devtron/pkg/app"
	"github.com/devtron-labs/devtron/pkg/deploymentGroup"
	"github.com/devtron-labs/devtron/pkg/pipeline"
	"github.com/devtron-labs/devtron/pkg/team"
	"github.com/devtron-labs/devtron/pkg/user"
	"github.com/devtron-labs/devtron/util/rbac"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type AppListingRestHandler interface {
	FetchAppsByEnvironment(w http.ResponseWriter, r *http.Request)
	FetchAppDetails(w http.ResponseWriter, r *http.Request)

	FetchAppTriggerView(w http.ResponseWriter, r *http.Request)
	FetchAppStageStatus(w http.ResponseWriter, r *http.Request)

	FetchOtherEnvironment(w http.ResponseWriter, r *http.Request)
	RedirectToLinkouts(w http.ResponseWriter, r *http.Request)
}

type AppListingRestHandlerImpl struct {
	application            application.ServiceClient
	appListingService      app.AppListingService
	teamService            team.TeamService
	enforcer               rbac.Enforcer
	pipeline               pipeline.PipelineBuilder
	logger                 *zap.SugaredLogger
	enforcerUtil           rbac.EnforcerUtil
	deploymentGroupService deploymentGroup.DeploymentGroupService
	userService            user.UserService
}

type AppStatus struct {
	name       string
	status     string
	message    string
	err        error
	conditions []v1alpha1.ApplicationCondition
}

func NewAppListingRestHandlerImpl(application application.ServiceClient,
	appListingService app.AppListingService,
	teamService team.TeamService,
	enforcer rbac.Enforcer,
	pipeline pipeline.PipelineBuilder,
	logger *zap.SugaredLogger, enforcerUtil rbac.EnforcerUtil,
	deploymentGroupService deploymentGroup.DeploymentGroupService, userService user.UserService) *AppListingRestHandlerImpl {
	appListingHandler := &AppListingRestHandlerImpl{
		application:            application,
		appListingService:      appListingService,
		logger:                 logger,
		teamService:            teamService,
		pipeline:               pipeline,
		enforcer:               enforcer,
		enforcerUtil:           enforcerUtil,
		deploymentGroupService: deploymentGroupService,
		userService:            userService,
	}
	return appListingHandler
}

func setupResponse(w *http.ResponseWriter, req *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
	(*w).Header().Set("Content-Type", "text/html; charset=utf-8")
}

func (handler AppListingRestHandlerImpl) FetchAppsByEnvironment(w http.ResponseWriter, r *http.Request) {
	//Allow CORS here By * or specific origin
	setupResponse(&w, r)
	token := r.Header.Get("token")
	t0 := time.Now()
	t1 := time.Now()
	handler.logger.Infow("api response time testing", "time", time.Now().String(), "stage", "1")
	userId, err := handler.userService.GetLoggedInUser(r)
	if userId == 0 || err != nil {
		writeJsonResp(w, err, "Unauthorized User", http.StatusUnauthorized)
		return
	}
	user, err := handler.userService.GetById(userId)
	if userId == 0 || err != nil {
		writeJsonResp(w, err, "Unauthorized User", http.StatusUnauthorized)
		return
	}
	userEmailId := strings.ToLower(user.EmailId)
	var fetchAppListingRequest app.FetchAppListingRequest
	decoder := json.NewDecoder(r.Body)
	err = decoder.Decode(&fetchAppListingRequest)
	if err != nil {
		handler.logger.Errorw("request err, FetchAppsByEnvironment", "err", err, "payload", fetchAppListingRequest)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}

	var dg *deploymentGroup.DeploymentGroupDTO
	if fetchAppListingRequest.DeploymentGroupId > 0 {
		dg, err = handler.deploymentGroupService.FindById(fetchAppListingRequest.DeploymentGroupId)
		if err != nil {
			handler.logger.Errorw("service err, FetchAppsByEnvironment", "err", err, "payload", fetchAppListingRequest)
			writeJsonResp(w, err, "", http.StatusInternalServerError)
		}
	}

	envContainers, err := handler.appListingService.FetchAppsByEnvironment(fetchAppListingRequest, w, r, token)
	if err != nil {
		handler.logger.Errorw("service err, FetchAppsByEnvironment", "err", err, "payload", fetchAppListingRequest)
		writeJsonResp(w, err, "", http.StatusInternalServerError)
	}
	t2 := time.Now()
	handler.logger.Infow("api response time testing", "time", time.Now().String(), "time diff", t2.Unix()-t1.Unix(), "stage", "2")
	t1 = t2

	isActionUserSuperAdmin, err := handler.userService.IsSuperAdmin(int(userId))
	if err != nil {
		handler.logger.Errorw("request err, FetchAppsByEnvironment", "err", err, "userId", userId)
		writeJsonResp(w, err, "Failed to check is super admin", http.StatusInternalServerError)
		return
	}
	appEnvContainers := make([]*bean.AppEnvironmentContainer, 0)
	if isActionUserSuperAdmin {
		appEnvContainers = append(appEnvContainers, envContainers...)
	} else {
		uniqueTeams := make(map[int]string)
		authorizedTeams := make(map[int]bool)
		for _, envContainer := range envContainers {
			if _, ok := uniqueTeams[envContainer.TeamId]; !ok {
				uniqueTeams[envContainer.TeamId] = envContainer.TeamName
			}
		}
		for teamId, teamName := range uniqueTeams {
			object := strings.ToLower(teamName)
			if ok := handler.enforcer.EnforceByEmail(userEmailId, rbac.ResourceTeam, rbac.ActionGet, object); ok {
				authorizedTeams[teamId] = true
			}
		}
		filteredAppEnvContainers := make([]*bean.AppEnvironmentContainer, 0)
		for _, envContainer := range envContainers {
			if _, ok := authorizedTeams[envContainer.TeamId]; ok {
				filteredAppEnvContainers = append(filteredAppEnvContainers, envContainer)
			}
		}
		for _, filteredAppEnvContainer := range filteredAppEnvContainers {
			if fetchAppListingRequest.DeploymentGroupId > 0 {
				if filteredAppEnvContainer.EnvironmentId != 0 && filteredAppEnvContainer.EnvironmentId != dg.EnvironmentId {
					continue
				}
			}
			object := fmt.Sprintf("%s/%s", filteredAppEnvContainer.TeamName, filteredAppEnvContainer.AppName)
			object = strings.ToLower(object)
			if ok := handler.enforcer.EnforceByEmail(userEmailId, rbac.ResourceApplications, rbac.ActionGet, object); ok {
				appEnvContainers = append(appEnvContainers, filteredAppEnvContainer)
			}
		}
	}
	t2 = time.Now()
	handler.logger.Infow("api response time testing", "time", time.Now().String(), "time diff", t2.Unix()-t1.Unix(), "stage", "3")
	t1 = t2
	apps, err := handler.appListingService.BuildAppListingResponse(fetchAppListingRequest, appEnvContainers)
	if err != nil {
		handler.logger.Errorw("service err, FetchAppsByEnvironment", "err", err, "payload", fetchAppListingRequest)
		writeJsonResp(w, err, "", http.StatusInternalServerError)
	}

	// Apply pagination
	appsCount := len(apps)
	offset := fetchAppListingRequest.Offset
	limit := fetchAppListingRequest.Size

	if offset+limit <= len(apps) {
		apps = apps[offset : offset+limit]
	} else {
		apps = apps[offset:]
	}

	appContainerResponse := bean.AppContainerResponse{
		AppContainers: apps,
		AppCount:      appsCount,
	}
	if fetchAppListingRequest.DeploymentGroupId > 0 {
		var ciMaterialDTOs []bean.CiMaterialDTO
		for _, ci := range dg.CiMaterialDTOs {
			ciMaterialDTOs = append(ciMaterialDTOs, bean.CiMaterialDTO{
				Name:        ci.Name,
				SourceValue: ci.SourceValue,
				SourceType:  ci.SourceType,
			})
		}
		appContainerResponse.DeploymentGroupDTO = bean.DeploymentGroupDTO{
			Id:             dg.Id,
			Name:           dg.Name,
			AppCount:       dg.AppCount,
			NoOfApps:       dg.NoOfApps,
			EnvironmentId:  dg.EnvironmentId,
			CiPipelineId:   dg.CiPipelineId,
			CiMaterialDTOs: ciMaterialDTOs,
		}
	}
	t2 = time.Now()
	handler.logger.Infow("api response time testing", "time", time.Now().String(), "time diff", t2.Unix()-t1.Unix(), "stage", "4")
	t1 = t2
	handler.logger.Infow("api response time testing", "total time", time.Now().String(), "total time", t1.Unix()-t0.Unix())
	writeJsonResp(w, err, appContainerResponse, http.StatusOK)
}

func (handler AppListingRestHandlerImpl) FetchAppDetails(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	token := r.Header.Get("token")
	appId, err := strconv.Atoi(vars["app-id"])
	if err != nil {
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}
	envId, err := strconv.Atoi(vars["env-id"])
	if err != nil {
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}
	appDetail, err := handler.appListingService.FetchAppDetails(appId, envId)
	if err != nil {
		handler.logger.Errorw("service err, FetchAppDetails", "err", err, "appId", appId, "envId", envId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return
	}

	object := handler.enforcerUtil.GetAppRBACNameByAppId(appId)
	if ok := handler.enforcer.Enforce(token, rbac.ResourceApplications, rbac.ActionGet, object); !ok {
		writeJsonResp(w, fmt.Errorf("unauthorized user"), nil, http.StatusForbidden)
		return
	}

	if len(appDetail.AppName) > 0 && len(appDetail.EnvironmentName) > 0 {
		//RBAC enforcer Ends
		acdAppName := appDetail.AppName + "-" + appDetail.EnvironmentName
		query := &application2.ResourcesQuery{
			ApplicationName: &acdAppName,
		}
		ctx, cancel := context.WithCancel(r.Context())
		if cn, ok := w.(http.CloseNotifier); ok {
			go func(done <-chan struct{}, closed <-chan bool) {
				select {
				case <-done:
				case <-closed:
					cancel()
				}
			}(ctx.Done(), cn.CloseNotify())
		}
		ctx = context.WithValue(ctx, "token", token)
		defer cancel()
		start := time.Now()
		resp, err := handler.application.ResourceTree(ctx, query)
		elapsed := time.Since(start)
		if err != nil {
			handler.logger.Errorw("service err, FetchAppDetails, resource tree", "err", err, "app", appId, "env", envId)
			err = &util.ApiError{
				Code:            constants.AppDetailResourceTreeNotFound,
				InternalMessage: "app detail fetched, failed to get resource tree from acd",
				UserMessage:     "Error fetching detail, if you have recently created this deployment pipeline please try after sometime.",
			}
			writeJsonResp(w, err, "", http.StatusInternalServerError)
			return
		}
		if resp.Status == v1alpha1.HealthStatusHealthy {
			status, err := handler.appListingService.ISLastReleaseStopType(appId, envId)
			if err != nil {
				handler.logger.Errorw("service err, FetchAppDetails", "err", err, "app", appId, "env", envId)
			} else if status {
				resp.Status = application.HIBERNATING
			}
		}
		handler.logger.Debugf("FetchAppDetails, time elapsed %s in fetching application %s for environment %s", elapsed, appId, envId)

		if resp.Status == v1alpha1.HealthStatusDegraded {
			count, err := handler.appListingService.GetReleaseCount(appId, envId)
			if err != nil {
				handler.logger.Errorw("service err, FetchAppDetails, release count", "err", err, "app", appId, "env", envId)
			} else if count == 0 {
				resp.Status = app.NotDeployed
			}
		}
		appDetail.ResourceTree = resp
		handler.logger.Debugf("application %s in environment %s had status %+v\n", appId, envId, resp)
	} else {
		handler.logger.Warnw("appName and envName not found - avoiding resource tree call", "app", appDetail.AppName, "env", appDetail.EnvironmentName)
	}
	writeJsonResp(w, err, appDetail, http.StatusOK)
}

func (handler AppListingRestHandlerImpl) FetchAppTriggerView(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	token := r.Header.Get("token")
	appId, err := strconv.Atoi(vars["app-id"])
	if err != nil {
		handler.logger.Errorw("request err, FetchAppTriggerView", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}
	handler.logger.Debugw("request payload, FetchAppTriggerView", "appId", appId)

	triggerView, err := handler.appListingService.FetchAppTriggerView(appId)
	if err != nil {
		handler.logger.Errorw("service err, FetchAppTriggerView", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return
	}

	//TODO: environment based auth, purge data of environment on which user doesnt have access, only show environment name
	// RBAC enforcer applying
	if len(triggerView) > 0 {
		object := handler.enforcerUtil.GetAppRBACName(triggerView[0].AppName)
		if ok := handler.enforcer.Enforce(token, rbac.ResourceApplications, rbac.ActionGet, object); !ok {
			writeJsonResp(w, err, "Unauthorized User", http.StatusForbidden)
			return
		}
	}
	//RBAC enforcer Ends

	ctx, cancel := context.WithCancel(r.Context())
	if cn, ok := w.(http.CloseNotifier); ok {
		go func(done <-chan struct{}, closed <-chan bool) {
			select {
			case <-done:
			case <-closed:
				cancel()
			}
		}(ctx.Done(), cn.CloseNotify())
	}
	ctx = context.WithValue(ctx, "token", token)
	defer cancel()

	response := make(chan AppStatus)
	qCount := len(triggerView)
	responses := map[string]AppStatus{}

	for i := 0; i < len(triggerView); i++ {
		acdAppName := triggerView[i].AppName + "-" + triggerView[i].EnvironmentName
		go func(pipelineName string) {
			ctxt, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			query := application2.ApplicationQuery{Name: &pipelineName}
			app, conn, err := handler.application.Watch(ctxt, &query)
			defer conn.Close()
			if err != nil {
				response <- AppStatus{name: pipelineName, status: "", message: "", err: err, conditions: make([]v1alpha1.ApplicationCondition, 0)}
				return
			}
			if app != nil {
				resp, err := app.Recv()
				if err != nil {
					response <- AppStatus{name: pipelineName, status: "", message: "", err: err, conditions: make([]v1alpha1.ApplicationCondition, 0)}
					return
				}
				if resp != nil {
					healthStatus := resp.Application.Status.Health.Status
					status := AppStatus{
						name:       pipelineName,
						status:     healthStatus,
						message:    resp.Application.Status.Health.Message,
						err:        nil,
						conditions: resp.Application.Status.Conditions,
					}
					response <- status
					return
				}
				response <- AppStatus{name: pipelineName, status: "", message: "", err: fmt.Errorf("Missing Application"), conditions: make([]v1alpha1.ApplicationCondition, 0)}
				return
			}
			response <- AppStatus{name: pipelineName, status: "", message: "", err: fmt.Errorf("Connection Closed by Client"), conditions: make([]v1alpha1.ApplicationCondition, 0)}

		}(acdAppName)
	}
	rCount := 0

	for {
		select {
		case msg, ok := <-response:
			if ok {
				if msg.err == nil {
					responses[msg.name] = msg
				}
			}
			rCount++
		}
		if qCount == rCount {
			break
		}
	}

	for i := 0; i < len(triggerView); i++ {
		acdAppName := triggerView[i].AppName + "-" + triggerView[i].EnvironmentName
		if val, ok := responses[acdAppName]; ok {
			status := val.status
			conditions := val.conditions
			for _, condition := range conditions {
				if condition.Type != v1alpha1.ApplicationConditionSharedResourceWarning {
					status = "Degraded"
				}
			}
			triggerView[i].Status = status
			triggerView[i].StatusMessage = val.message
			triggerView[i].Conditions = val.conditions
		}
		if triggerView[i].Status == "" {
			triggerView[i].Status = "Unknown"
		}
		if triggerView[i].Status == v1alpha1.HealthStatusDegraded {
			triggerView[i].Status = "Not Deployed"
		}
	}
	writeJsonResp(w, err, triggerView, http.StatusOK)
}

func (handler AppListingRestHandlerImpl) FetchAppStageStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appId, err := strconv.Atoi(vars["app-id"])
	if err != nil {
		handler.logger.Errorw("request err, FetchAppStageStatus", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}
	handler.logger.Infow("request payload, FetchAppStageStatus", "appId", appId)
	token := r.Header.Get("token")
	app, err := handler.pipeline.GetApp(appId)
	if err != nil {
		handler.logger.Errorw("service err, FetchAppStageStatus", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}

	// RBAC enforcer applying
	object := handler.enforcerUtil.GetAppRBACName(app.AppName)
	if ok := handler.enforcer.Enforce(token, rbac.ResourceApplications, rbac.ActionGet, object); !ok {
		writeJsonResp(w, fmt.Errorf("unauthorized user"), "Unauthorized User", http.StatusForbidden)
		return
	}
	//RBAC enforcer Ends

	triggerView, err := handler.appListingService.FetchAppStageStatus(appId)
	if err != nil {
		handler.logger.Errorw("service err, FetchAppStageStatus", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return
	}
	writeJsonResp(w, err, triggerView, http.StatusOK)
}

func (handler AppListingRestHandlerImpl) FetchOtherEnvironment(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appId, err := strconv.Atoi(vars["app-id"])
	if err != nil {
		handler.logger.Errorw("request err, FetchOtherEnvironment", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}
	token := r.Header.Get("token")
	app, err := handler.pipeline.GetApp(appId)
	if err != nil {
		handler.logger.Errorw("service err, FetchOtherEnvironment", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}

	// RBAC enforcer applying
	object := handler.enforcerUtil.GetAppRBACName(app.AppName)
	if ok := handler.enforcer.Enforce(token, rbac.ResourceApplications, rbac.ActionGet, object); !ok {
		writeJsonResp(w, err, "unauthorized user", http.StatusForbidden)
		return
	}
	//RBAC enforcer Ends

	otherEnvironment, err := handler.appListingService.FetchOtherEnvironment(appId)
	if err != nil {
		handler.logger.Errorw("service err, FetchOtherEnvironment", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return
	}

	//TODO - rbac env level

	writeJsonResp(w, err, otherEnvironment, http.StatusOK)
}

func (handler AppListingRestHandlerImpl) RedirectToLinkouts(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("token")
	vars := mux.Vars(r)
	Id, err := strconv.Atoi(vars["Id"])
	if err != nil {
		handler.logger.Errorw("request err, RedirectToLinkouts", "err", err, "id", Id)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}
	appId, err := strconv.Atoi(vars["appId"])
	if err != nil {
		handler.logger.Errorw("request err, RedirectToLinkouts", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}
	envId, err := strconv.Atoi(vars["envId"])
	if err != nil {
		handler.logger.Errorw("request err, RedirectToLinkouts", "err", err, "envId", envId)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}
	podName := vars["podName"]
	containerName := vars["containerName"]
	app, err := handler.pipeline.GetApp(appId)
	if err != nil {
		handler.logger.Errorw("bad request", "err", err)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}

	// RBAC enforcer applying
	object := handler.enforcerUtil.GetAppRBACName(app.AppName)
	if ok := handler.enforcer.Enforce(token, rbac.ResourceApplications, rbac.ActionGet, object); !ok {
		writeJsonResp(w, err, "unauthorized user", http.StatusForbidden)
		return
	}
	//RBAC enforcer Ends

	link, err := handler.appListingService.RedirectToLinkouts(Id, appId, envId, podName, containerName)
	if err != nil || len(link) == 0 {
		handler.logger.Errorw("service err, RedirectToLinkouts", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, link, http.StatusOK)
}
