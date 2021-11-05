package gorm

import (
	"strings"
	"time"

	"github.com/porter-dev/porter/api/types"
	"github.com/porter-dev/porter/internal/models"
	"github.com/porter-dev/porter/internal/repository"
	"gorm.io/gorm"
)

// BuildEventRepository holds both EventContainer and SubEvent models
type BuildEventRepository struct {
	db *gorm.DB
}

// NewBuildEventRepository returns a BuildEventRepository which uses
// gorm.DB for querying the database
func NewBuildEventRepository(db *gorm.DB) repository.BuildEventRepository {
	return &BuildEventRepository{db}
}

func (repo BuildEventRepository) CreateEventContainer(am *models.EventContainer) (*models.EventContainer, error) {
	if err := repo.db.Create(am).Error; err != nil {
		return nil, err
	}
	return am, nil
}

func (repo BuildEventRepository) CreateSubEvent(am *models.SubEvent) (*models.SubEvent, error) {
	if err := repo.db.Create(am).Error; err != nil {
		return nil, err
	}
	return am, nil
}

func (repo BuildEventRepository) ReadEventsByContainerID(id uint) ([]*models.SubEvent, error) {
	var events []*models.SubEvent
	if err := repo.db.Where("event_container_id = ?", id).Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func (repo BuildEventRepository) ReadEventContainer(id uint) (*models.EventContainer, error) {
	container := &models.EventContainer{}
	if err := repo.db.Where("id = ?", id).First(&container).Error; err != nil {
		return nil, err
	}
	return container, nil
}

func (repo BuildEventRepository) ReadSubEvent(id uint) (*models.SubEvent, error) {
	event := &models.SubEvent{}
	if err := repo.db.Where("id = ?", id).First(&event).Error; err != nil {
		return nil, err
	}
	return event, nil
}

// AppendEvent will check if subevent with same (id, index) already exists
// if yes, overrite it, otherwise make a new subevent
func (repo BuildEventRepository) AppendEvent(container *models.EventContainer, event *models.SubEvent) error {
	event.EventContainerID = container.ID
	return repo.db.Create(event).Error
}

// KubeEventRepository uses gorm.DB for querying the database
type KubeEventRepository struct {
	db  *gorm.DB
	key *[32]byte
}

// NewKubeEventRepository returns an KubeEventRepository which uses
// gorm.DB for querying the database. It accepts an encryption key to encrypt
// sensitive data
func NewKubeEventRepository(db *gorm.DB, key *[32]byte) repository.KubeEventRepository {
	return &KubeEventRepository{db, key}
}

// CreateEvent creates a new kube auth mechanism
func (repo *KubeEventRepository) CreateEvent(
	event *models.KubeEvent,
) (*models.KubeEvent, error) {
	if err := repo.db.Create(event).Error; err != nil {
		return nil, err
	}

	return event, nil
}

// ReadEvent finds an event by id
func (repo *KubeEventRepository) ReadEvent(
	id, projID, clusterID uint,
) (*models.KubeEvent, error) {
	event := &models.KubeEvent{}

	if err := repo.db.Preload("SubEvents").Where(
		"id = ? AND project_id = ? AND cluster_id = ?",
		id,
		projID,
		clusterID,
	).First(&event).Error; err != nil {
		return nil, err
	}

	// subEvents := make([]models.KubeSubEvent, 0)

	// if err := repo.db.Where("kube_event_id = ?", event.ID).Find(&subEvents).Error; err != nil {
	// 	return nil, err
	// }

	// event.SubEvents = subEvents

	return event, nil
}

// ReadEventByGroup finds an event by a set of options which group events together
func (repo *KubeEventRepository) ReadEventByGroup(
	projID uint,
	clusterID uint,
	opts *types.GroupOptions,
) (*models.KubeEvent, error) {
	event := &models.KubeEvent{}

	query := repo.db.Debug().Preload("SubEvents").
		Where("project_id = ? AND cluster_id = ? AND name = ? AND resource_type = ?", projID, clusterID, opts.Name, opts.ResourceType)

	// construct query for timestamp
	query = query.Where(
		"updated_at >= ?", opts.ThresholdTime,
	)

	if opts.Namespace != "" {
		query = query.Where(
			"namespace = ?",
			strings.ToLower(opts.Namespace),
		)
	}

	if err := query.First(&event).Error; err != nil {
		return nil, err
	}

	return event, nil
}

// ListEventsByProjectID finds all events for a given project id
// with the given options
func (repo *KubeEventRepository) ListEventsByProjectID(
	projectID uint,
	clusterID uint,
	opts *types.ListKubeEventRequest,
) ([]*models.KubeEvent, error) {
	listOpts := opts

	if listOpts.Limit == 0 {
		listOpts.Limit = 50
	}

	events := []*models.KubeEvent{}

	// preload the subevents
	query := repo.db.Preload("SubEvents").Where("project_id = ? AND cluster_id = ?", projectID, clusterID)

	if listOpts.OwnerName != "" && listOpts.OwnerType != "" {
		query = query.Where(
			"owner_name = ? AND owner_type = ?",
			listOpts.OwnerName,
			listOpts.OwnerType,
		)
	}

	if listOpts.ResourceType != "" {
		query = query.Where(
			"resource_type = ?",
			listOpts.ResourceType,
		)
	}

	query = query.Limit(listOpts.Limit).Offset(listOpts.Skip)

	if listOpts.SortBy == "timestamp" {
		// sort by the updated_at field
		query = query.Order("updated_at desc").Order("id desc")
	}

	if err := query.Find(&events).Error; err != nil {
		return nil, err
	}

	return events, nil
}

// AppendSubEvent will add a subevent to an existing event
func (repo *KubeEventRepository) AppendSubEvent(event *models.KubeEvent, subEvent *models.KubeSubEvent) error {
	subEvent.KubeEventID = event.ID

	if err := repo.db.Create(subEvent).Error; err != nil {
		return err
	}

	event.UpdatedAt = time.Now()

	return repo.db.Save(event).Error
}

// DeleteEvent deletes an event by ID
func (repo *KubeEventRepository) DeleteEvent(
	id uint,
) error {
	if err := repo.db.Preload("SubEvents").Where("id = ?", id).Delete(&models.KubeEvent{}).Error; err != nil {
		return err
	}

	return nil
}
