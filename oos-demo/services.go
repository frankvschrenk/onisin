package main

// services.go — Start functions for each managed service.
//
// oos-demo manages two things:
//   - the embedded IAM (started in-process as a goroutine; see iam package)
//   - the oosp backend (started as a native subprocess)
//
// LLM inference is provided by Ollama and started independently by the user.
// All services speak plain HTTP; TLS is the job of the production
// environment (Linkerd in k8s), not the demo.

import (
	"fmt"

	"onisin.com/oos-demo/iam"
)

// startIAM boots the embedded demo OIDC provider.
//
// Configured users come from [[dex.users]] in demo.toml (the section
// keeps its historical name for compatibility; it simply lists the
// demo identities now). Group assignment is hard-coded on the username
// convention: "admin" -> oos-admin, everything else -> oos-user.
func (m *Manager) startIAM() error {
	users := make([]iam.User, 0, len(m.cfg.Dex.Users))
	for _, u := range m.cfg.Dex.Users {
		users = append(users, iam.User{
			Email:    u.Email,
			Username: usernameFromEmail(u.Email),
			Groups:   []string{groupFor(u.Email)},
		})
	}

	srv, err := iam.Start(iam.Config{
		Port:  m.cfg.Dex.Port,
		Users: users,
	})
	if err != nil {
		return fmt.Errorf("iam: %w", err)
	}
	m.iam = srv
	return nil
}

// startOOSP starts the oosp backend as a managed subprocess.
func (m *Manager) startOOSP() error {
	pg := m.cfg.PostgreSQL

	// Build the oosp DSN with the dedicated oosp user.
	dsn := fmt.Sprintf("host=localhost port=%d user=%s password=%s dbname=%s sslmode=disable",
		pg.Port, "oosp", pg.AppUsers["oosp"], pg.Database)

	return m.startProcess("oosp", "oosp", []string{"-u"}, []string{
		fmt.Sprintf("OOSP_SERVER_ADDR=:%d", m.cfg.OOSP.Port),
		fmt.Sprintf("OOSP_DSN=%s", dsn),
		fmt.Sprintf("OOSP_LLM_URL=%s", m.cfg.LLM.URL),
		fmt.Sprintf("OOSP_EMBED_MODEL=%s", m.cfg.LLM.EmbedModel),
		"OOSP_DEBUG=true",
	})
}

// usernameFromEmail takes the local part of an email as the username.
func usernameFromEmail(email string) string {
	for i, c := range email {
		if c == '@' {
			return email[:i]
		}
	}
	return email
}

// groupFor maps a demo user's email to an oos group. The convention is
// "admin" -> oos-admin, everything else -> oos-user. Real group
// management is the job of a real IAM (Keycloak, Zitadel, Dex) behind
// oosp in production.
func groupFor(email string) string {
	if usernameFromEmail(email) == "admin" {
		return "oos-admin"
	}
	return "oos-user"
}
