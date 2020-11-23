package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi"
	"github.com/porter-dev/porter/internal/forms"
	"github.com/porter-dev/porter/internal/models"
)

// Enumeration of user API error codes, represented as int64
const (
	ErrProjectDecode ErrorCode = iota + 600
	ErrProjectValidateFields
	ErrProjectDataRead
)

// HandleCreateProject validates a project form entry, converts the project to a gorm
// model, and saves the user to the database
func (app *App) HandleCreateProject(w http.ResponseWriter, r *http.Request) {
	session, err := app.store.Get(r, app.cookieName)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	userID, _ := session.Values["user_id"].(uint)

	form := &forms.CreateProjectForm{}

	// decode from JSON to form value
	if err := json.NewDecoder(r.Body).Decode(form); err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	// validate the form
	if err := app.validator.Struct(form); err != nil {
		app.handleErrorFormValidation(err, ErrProjectValidateFields, w)
		return
	}

	// convert the form to a project model
	projModel, err := form.ToProject(app.repo.Project)

	if err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	// handle write to the database
	projModel, err = app.repo.Project.CreateProject(projModel)

	if err != nil {
		app.handleErrorDataWrite(err, w)
		return
	}

	// create a new Role with the user as the admin
	_, err = app.repo.Project.CreateProjectRole(projModel, &models.Role{
		UserID:    userID,
		ProjectID: projModel.ID,
		Kind:      models.RoleAdmin,
	})

	if err != nil {
		app.handleErrorDataWrite(err, w)
		return
	}

	app.logger.Info().Msgf("New project created: %d", projModel.ID)

	w.WriteHeader(http.StatusCreated)

	projExt := projModel.Externalize()

	if err := json.NewEncoder(w).Encode(projExt); err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}
}

// HandleReadProject returns an externalized Project (models.ProjectExternal)
// based on an ID
func (app *App) HandleReadProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "project_id"), 0, 64)

	if err != nil || id == 0 {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	proj, err := app.repo.Project.ReadProject(uint(id))

	if err != nil {
		app.handleErrorRead(err, ErrProjectDataRead, w)
		return
	}

	projExt := proj.Externalize()

	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(projExt); err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}
}

// HandleReadProjectCluster reads a cluster by id
func (app *App) HandleReadProjectCluster(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "cluster_id"), 0, 64)

	if err != nil || id == 0 {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	cluster, err := app.repo.Cluster.ReadCluster(uint(id))

	if err != nil {
		app.handleErrorRead(err, ErrProjectDataRead, w)
		return
	}

	clusterExt := cluster.Externalize()

	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(clusterExt); err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}
}

// HandleListProjectClusters returns a list of clusters that have linked Integrations.
func (app *App) HandleListProjectClusters(w http.ResponseWriter, r *http.Request) {
	projID, err := strconv.ParseUint(chi.URLParam(r, "project_id"), 0, 64)

	if err != nil || projID == 0 {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	clusters, err := app.repo.Cluster.ListClustersByProjectID(uint(projID))

	if err != nil {
		app.handleErrorRead(err, ErrProjectDataRead, w)
		return
	}

	extClusters := make([]*models.ClusterExternal, 0)

	for _, cluster := range clusters {
		extClusters = append(extClusters, cluster.Externalize())
	}

	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(extClusters); err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}
}

// HandleCreateProjectClusterCandidates handles the creation of ClusterCandidates using
// a kubeconfig and a project id
func (app *App) HandleCreateProjectClusterCandidates(w http.ResponseWriter, r *http.Request) {
	projID, err := strconv.ParseUint(chi.URLParam(r, "project_id"), 0, 64)

	if err != nil || projID == 0 {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	form := &forms.CreateClusterCandidatesForm{
		ProjectID: uint(projID),
	}

	// decode from JSON to form value
	if err := json.NewDecoder(r.Body).Decode(form); err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	// validate the form
	if err := app.validator.Struct(form); err != nil {
		app.handleErrorFormValidation(err, ErrProjectValidateFields, w)
		return
	}

	// convert the form to a ClusterCandidate
	ccs, err := form.ToClusterCandidates(app.isLocal)

	if err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	extClusters := make([]*models.ClusterCandidateExternal, 0)

	session, err := app.store.Get(r, app.cookieName)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	userID, _ := session.Values["user_id"].(uint)

	for _, cc := range ccs {
		// handle write to the database
		cc, err = app.repo.Cluster.CreateClusterCandidate(cc)

		if err != nil {
			app.handleErrorDataWrite(err, w)
			return
		}

		app.logger.Info().Msgf("New cluster candidate created: %d", cc.ID)

		// if the ClusterCandidate does not have any actions to perform, create the Cluster
		// automatically
		if len(cc.Resolvers) == 0 {
			// we query the repo again to get the decrypted version of the cluster candidate
			cc, err = app.repo.Cluster.ReadClusterCandidate(cc.ID)

			if err != nil {
				app.handleErrorDataRead(err, w)
				return
			}

			clusterForm := &forms.ResolveClusterForm{
				Resolver:           &models.ClusterResolverAll{},
				ClusterCandidateID: cc.ID,
				ProjectID:          uint(projID),
				UserID:             userID,
			}

			err := clusterForm.ResolveIntegration(*app.repo)

			if err != nil {
				app.handleErrorDataWrite(err, w)
				return
			}

			cluster, err := clusterForm.ResolveCluster(*app.repo)

			if err != nil {
				app.handleErrorDataWrite(err, w)
				return
			}

			cc, err = app.repo.Cluster.UpdateClusterCandidateCreatedClusterID(cc.ID, cluster.ID)

			if err != nil {
				app.handleErrorDataWrite(err, w)
				return
			}

			app.logger.Info().Msgf("New cluster created: %d", cluster.ID)
		}

		extClusters = append(extClusters, cc.Externalize())
	}

	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(extClusters); err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}
}

// HandleListProjectClusterCandidates returns a list of externalized ClusterCandidates
// ([]models.ClusterCandidateExternal) based on a project ID
func (app *App) HandleListProjectClusterCandidates(w http.ResponseWriter, r *http.Request) {
	projID, err := strconv.ParseUint(chi.URLParam(r, "project_id"), 0, 64)

	if err != nil || projID == 0 {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	ccs, err := app.repo.Cluster.ListClusterCandidatesByProjectID(uint(projID))

	if err != nil {
		app.handleErrorRead(err, ErrProjectDataRead, w)
		return
	}

	extCCs := make([]*models.ClusterCandidateExternal, 0)

	for _, cc := range ccs {
		extCCs = append(extCCs, cc.Externalize())
	}

	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(extCCs); err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}
}

// HandleResolveClusterCandidate accepts a list of resolving objects (ClusterResolver)
// for a given ClusterCandidate, which "resolves" that ClusterCandidate and creates a
// Cluster for a specific project
func (app *App) HandleResolveClusterCandidate(w http.ResponseWriter, r *http.Request) {
	projID, err := strconv.ParseUint(chi.URLParam(r, "project_id"), 0, 64)

	if err != nil || projID == 0 {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	candID, err := strconv.ParseUint(chi.URLParam(r, "candidate_id"), 0, 64)

	if err != nil || projID == 0 {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	session, err := app.store.Get(r, app.cookieName)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	userID, _ := session.Values["user_id"].(uint)

	// decode actions from request
	resolver := &models.ClusterResolverAll{}

	if err := json.NewDecoder(r.Body).Decode(resolver); err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	clusterResolver := &forms.ResolveClusterForm{
		Resolver:           resolver,
		ClusterCandidateID: uint(candID),
		ProjectID:          uint(projID),
		UserID:             userID,
	}

	err = clusterResolver.ResolveIntegration(*app.repo)

	if err != nil {
		app.handleErrorDataWrite(err, w)
		return
	}

	cluster, err := clusterResolver.ResolveCluster(*app.repo)

	if err != nil {
		app.handleErrorDataWrite(err, w)
		return
	}

	_, err = app.repo.Cluster.UpdateClusterCandidateCreatedClusterID(uint(candID), cluster.ID)

	if err != nil {
		app.handleErrorDataWrite(err, w)
		return
	}

	app.logger.Info().Msgf("New cluster created: %d", cluster.ID)

	clusterExt := cluster.Externalize()

	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(clusterExt); err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}
}

// HandleDeleteProject deletes a project from the db, reading from the project_id
// in the URL param
func (app *App) HandleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "project_id"), 0, 64)

	if err != nil || id == 0 {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}

	proj, err := app.repo.Project.ReadProject(uint(id))

	if err != nil {
		app.handleErrorRead(err, ErrProjectDataRead, w)
		return
	}

	proj, err = app.repo.Project.DeleteProject(proj)

	if err != nil {
		app.handleErrorRead(err, ErrProjectDataRead, w)
		return
	}

	projExternal := proj.Externalize()

	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(projExternal); err != nil {
		app.handleErrorFormDecoding(err, ErrProjectDecode, w)
		return
	}
}
