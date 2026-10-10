package application

import (
	"context"
	"fmt"
	"net/mail"
	"strings"

	"github.com/iamartinlong/Argus/internal/domain"
)

type ContactService struct {
	contacts domain.ContactStore
	audit    domain.AuditStore
	clock    Clock
}

func NewContactService(contacts domain.ContactStore, audit domain.AuditStore, clock Clock) *ContactService {
	return &ContactService{contacts: contacts, audit: audit, clock: clock}
}

type ContactInput struct {
	Name  string
	Email string
	Phone string
}

func (s *ContactService) List(ctx context.Context) ([]*domain.ContactPerson, error) {
	return s.contacts.List(ctx)
}

func (s *ContactService) Create(ctx context.Context, input ContactInput) (*domain.ContactPerson, error) {
	if err := validateContact(input); err != nil {
		return nil, err
	}
	contact := &domain.ContactPerson{
		ID:        newID(),
		Name:      strings.TrimSpace(input.Name),
		Email:     strings.TrimSpace(input.Email),
		Phone:     strings.TrimSpace(input.Phone),
		CreatedAt: s.clock.Now(),
		UpdatedAt: s.clock.Now(),
	}
	if err := s.contacts.Create(ctx, contact); err != nil {
		return nil, err
	}
	s.auditAction(ctx, nil, domain.AuditContactCreate, contact.Name, contact.Email)
	return contact, nil
}

func (s *ContactService) Update(ctx context.Context, id string, input ContactInput) (*domain.ContactPerson, error) {
	if err := validateContact(input); err != nil {
		return nil, err
	}
	contact, err := s.contacts.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	contact.Name = strings.TrimSpace(input.Name)
	contact.Email = strings.TrimSpace(input.Email)
	contact.Phone = strings.TrimSpace(input.Phone)
	contact.UpdatedAt = s.clock.Now()
	if err := s.contacts.Update(ctx, contact); err != nil {
		return nil, err
	}
	s.auditAction(ctx, nil, domain.AuditContactUpdate, contact.Name, contact.Email)
	return contact, nil
}

func (s *ContactService) Delete(ctx context.Context, id string) error {
	contact, err := s.contacts.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.contacts.Delete(ctx, id); err != nil {
		return err
	}
	s.auditAction(ctx, nil, domain.AuditContactDelete, contact.Name, contact.Email)
	return nil
}

func validateContact(input ContactInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: 申请人姓名不能为空", domain.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Email) == "" {
		return fmt.Errorf("%w: 申请人邮箱不能为空", domain.ErrInvalidArgument)
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(input.Email)); err != nil {
		return fmt.Errorf("%w: 申请人邮箱格式不正确", domain.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Phone) == "" {
		return fmt.Errorf("%w: 申请人电话不能为空", domain.ErrInvalidArgument)
	}
	return nil
}

func (s *ContactService) auditAction(ctx context.Context, actor *domain.User, action domain.AuditAction, resource, detail string) {
	userID, username := "", "system"
	if actor != nil {
		userID, username = actor.ID, actor.Username
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		UserID:    userID,
		Username:  username,
		Action:    action,
		Resource:  resource,
		Detail:    detail,
		CreatedAt: s.clock.Now(),
	})
}