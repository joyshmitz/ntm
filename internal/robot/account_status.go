package robot

import (
	"context"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tools"
)

// AccountStatusOutput represents the response from --robot-account-status
type AccountStatusOutput struct {
	RobotResponse
	Accounts map[string]ProviderStatus `json:"accounts"`
}

// ProviderStatus contains status information for a single provider
type ProviderStatus struct {
	Current           string `json:"current"`
	UsagePercent      int    `json:"usage_percent,omitempty"`
	LimitReset        string `json:"limit_reset,omitempty"`
	AvailableAccounts int    `json:"available_accounts"`
	RateLimited       bool   `json:"rate_limited,omitempty"`
}

// AccountStatusOptions contains options for the account status command
type AccountStatusOptions struct {
	Provider string // Optional filter for a specific provider (claude, openai, gemini)
}

// GetAccountStatus returns account status information.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetAccountStatus(opts AccountStatusOptions) (*AccountStatusOutput, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adapter := tools.NewCAAMAdapter()

	// Check if CAAM is available
	if _, installed := adapter.Detect(); !installed {
		output := &AccountStatusOutput{
			RobotResponse: NewErrorResponse(nil, ErrCodeDependencyMissing, "Install caam to manage coding agent accounts"),
			Accounts:      make(map[string]ProviderStatus),
		}
		output.Error = "caam not installed"
		return output, nil
	}

	// Get all accounts from CAAM
	status, err := adapter.GetStatus(ctx)
	if err != nil {
		return &AccountStatusOutput{
			RobotResponse: NewErrorResponse(err, ErrCodeInternalError, "Check if caam is configured correctly"),
			Accounts:      make(map[string]ProviderStatus),
		}, nil
	}

	// Build per-provider status map
	providerAccounts := make(map[string][]tools.CAAMAccount)
	for _, acc := range status.Accounts {
		provider := canonicalRobotProvider(acc.Provider)
		providerAccounts[provider] = append(providerAccounts[provider], acc)
	}
	opts.Provider = canonicalRobotProvider(opts.Provider)

	// Build output
	output := &AccountStatusOutput{
		RobotResponse: NewRobotResponse(true),
		Accounts:      make(map[string]ProviderStatus),
	}

	for provider, accounts := range providerAccounts {
		// Filter by provider if specified
		if opts.Provider != "" && provider != opts.Provider {
			continue
		}

		provStatus := ProviderStatus{
			AvailableAccounts: len(accounts),
		}

		// Find the active/current account for this provider
		for _, acc := range accounts {
			if acc.Active {
				provStatus.Current = acc.Email
				if provStatus.Current == "" {
					provStatus.Current = acc.Name
				}
				if provStatus.Current == "" {
					provStatus.Current = acc.ID
				}
				provStatus.RateLimited = acc.RateLimited
				if !acc.CooldownUntil.IsZero() {
					provStatus.LimitReset = FormatTimestamp(acc.CooldownUntil)
				}
			}
		}

		output.Accounts[provider] = provStatus
	}

	// If a specific provider was requested but not found, still include it with zero values
	if opts.Provider != "" {
		if _, exists := output.Accounts[opts.Provider]; !exists {
			output.Accounts[opts.Provider] = ProviderStatus{
				AvailableAccounts: 0,
			}
		}
	}

	return output, nil
}

// PrintAccountStatus handles the --robot-account-status command.
// This is a thin wrapper around GetAccountStatus() for CLI output.
func PrintAccountStatus(opts AccountStatusOptions) error {
	output, err := GetAccountStatus(opts)
	if err != nil {
		return err
	}
	return outputJSON(output)
}
