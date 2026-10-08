package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/Paca-AI/api/internal/bootstrap/defaultroles"
	"github.com/Paca-AI/api/internal/config"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
)

// agentBotUsername is the login name of the built-in agent bot account.
const agentBotUsername = "_paca_agent_bot"

// platformRoleSeeder is the write side of startup seeding: the system roles
// and the bootstrap accounts' attachments. *pgRepo.RoleSeedRepository
// implements it; every method is idempotent.
type platformRoleSeeder interface {
	UpsertSystemRole(ctx context.Context, def pgRepo.SeedRole) (pgRepo.SeededRole, error)
	EnsureDefaultRole(ctx context.Context, fallback string) (bool, error)
	AttachPlatformIfMissing(ctx context.Context, userID, roleID uuid.UUID) (bool, error)
}

// policyInvalidator drops cached role policies; *iam.Authorizer implements it.
type policyInvalidator interface {
	Invalidate(roleIDs ...string)
}

// seedPlatformRoles makes the shipped platform roles (SUPER_ADMIN, ADMIN,
// USER) exist as system roles with the shipped policy, and makes sure one
// platform role is the default. It returns the ids of the shipped roles by
// name.
//
// Reconciliation is by name and touches nothing else: a platform role of the
// same name that migration 000064 converted from the legacy tables becomes
// the system role (same id, same holders); every other role — custom roles,
// converted project roles, project-owned roles — is left exactly as it is.
// A system role that exists keeps the policy and description it has, so an
// administrator's edits to it survive restarts and upgrades; system roles
// cannot be deleted. The policy cache is invalidated for every role that was
// created or changed.
func seedPlatformRoles(ctx context.Context, seeder platformRoleSeeder, inv policyInvalidator, log *slog.Logger) (map[string]uuid.UUID, error) {
	ids := map[string]uuid.UUID{}
	var touched []string
	for _, def := range defaultroles.Platform() {
		res, err := seeder.UpsertSystemRole(ctx, pgRepo.SeedRole{
			Name: def.Name, Description: def.Description, Policy: def.Policy,
		})
		if err != nil {
			return nil, fmt.Errorf("seed roles: %w", err)
		}
		ids[def.Name] = res.ID
		switch {
		case res.Created:
			touched = append(touched, res.ID.String())
			log.Info("system role created", "role", def.Name)
		case res.Changed:
			touched = append(touched, res.ID.String())
			log.Info("system role marked as built-in", "role", def.Name)
		}
	}
	if len(touched) > 0 && inv != nil {
		inv.Invalidate(touched...)
	}

	// New accounts and global agents start with the default role, so one must
	// exist. A default an administrator has chosen is never overridden.
	set, err := seeder.EnsureDefaultRole(ctx, defaultroles.User)
	if err != nil {
		return nil, fmt.Errorf("seed roles: default role: %w", err)
	}
	if set {
		log.Info("no default role was set; USER is now the default")
	}
	return ids, nil
}

// seedAdmin ensures the configured admin account exists and holds
// SUPER_ADMIN. A missing account is created with that role in the same
// transaction. An existing, non-deleted account keeps everything it has and
// gains SUPER_ADMIN if it lacks it; a soft-deleted one is left alone.
func seedAdmin(ctx context.Context, repo userdom.Repository, seeder platformRoleSeeder, superAdmin uuid.UUID, cfg config.AdminConfig, log *slog.Logger) error {
	existing, err := repo.FindByUsernameIncludingDeleted(ctx, cfg.Username)
	switch {
	case err == nil:
		if existing.DeletedAt != nil {
			return nil
		}
		added, aerr := seeder.AttachPlatformIfMissing(ctx, existing.ID, superAdmin)
		if aerr != nil {
			return fmt.Errorf("seed admin: attach SUPER_ADMIN: %w", aerr)
		}
		if added {
			log.Info("assigned SUPER_ADMIN role to admin user", "username", cfg.Username)
		}
		return nil
	case !errors.Is(err, userdom.ErrNotFound):
		return fmt.Errorf("seed admin: lookup: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("seed admin: hash password: %w", err)
	}
	now := time.Now()
	admin := &userdom.User{
		ID:           uuid.New(),
		Username:     cfg.Username,
		PasswordHash: string(hash),
		FullName:     "Admin",
		Roles:        []roledom.Summary{{ID: superAdmin, Name: defaultroles.SuperAdmin}},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.Create(ctx, admin); err != nil {
		return fmt.Errorf("seed admin: create: %w", err)
	}
	log.Info("admin account created", "username", cfg.Username)
	return nil
}

// seedAgentBotUser ensures the built-in agent bot user exists and holds
// SUPER_ADMIN. It is the identity of requests authenticated with
// AGENT_API_KEY; it can never log in with a password because its
// password_hash is set to an invalid value.
func seedAgentBotUser(ctx context.Context, repo userdom.Repository, seeder platformRoleSeeder, superAdmin uuid.UUID, log *slog.Logger) error {
	existing, err := repo.FindByUsernameIncludingDeleted(ctx, agentBotUsername)
	switch {
	case err == nil:
		if existing.DeletedAt != nil {
			return nil
		}
		if _, aerr := seeder.AttachPlatformIfMissing(ctx, existing.ID, superAdmin); aerr != nil {
			return fmt.Errorf("seed agent bot: attach SUPER_ADMIN: %w", aerr)
		}
		return nil
	case !errors.Is(err, userdom.ErrNotFound):
		return fmt.Errorf("seed agent bot: lookup: %w", err)
	}

	now := time.Now()
	bot := &userdom.User{
		ID:           agentBotUserID,
		Username:     agentBotUsername,
		PasswordHash: "!", // intentionally invalid — bot cannot log in with a password
		FullName:     "Paca Agent Bot",
		Roles:        []roledom.Summary{{ID: superAdmin, Name: defaultroles.SuperAdmin}},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.Create(ctx, bot); err != nil {
		return fmt.Errorf("seed agent bot: create: %w", err)
	}
	log.Info("agent bot user created")
	return nil
}
