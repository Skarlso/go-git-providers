/*
Copyright 2023 The Flux CD contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gitea

import (
	"context"
	"fmt"
	"strings"

	"code.gitea.io/sdk/gitea"
	"github.com/fluxcd/go-git-providers/gitprovider"
)

const (
	// DefaultDomain specifies the default domain used as the backend.
	DefaultDomain = "gitea.com"
	// ProviderID is the provider ID for Gitea.
	ProviderID = gitprovider.ProviderID("gitea")
)

// NewClient creates a new gitprovider.Client instance for Gitea API endpoints.
//
// Gitea Selfhosted can be used if you specify the domain using WithDomain.
func NewClient(token string, optFns ...gitprovider.ClientOption) (gitprovider.Client, error) {
	// Complete the options struct
	opts, err := gitprovider.MakeClientOptions(optFns...)
	if err != nil {
		return nil, err
	}

	// Create a *http.Client using the transport chain
	httpClient, err := gitprovider.BuildClientFromTransportChain(opts.GetTransportChain())
	if err != nil {
		return nil, err
	}

	domain := DefaultDomain
	if opts.Domain != nil {
		domain = *opts.Domain
	}
	baseURL := domain
	if !strings.Contains(domain, "://") {
		baseURL = fmt.Sprintf("https://%s/", domain)
	}

	gt, err := gitea.NewClient(baseURL, gitea.SetHTTPClient(httpClient), gitea.SetToken(token))
	if err != nil {
		return nil, err
	}
	// By default, turn destructive actions off. But allow overrides.
	destructiveActions := false
	if opts.EnableDestructiveAPICalls != nil {
		destructiveActions = *opts.EnableDestructiveAPICalls
	}

	return newClient(gt, domain, destructiveActions), nil
}

func newClient(c *gitea.Client, domain string, destructiveActions bool) *Client {
	ctx := &clientContext{c, domain, destructiveActions}
	return &Client{
		clientContext: ctx,
		orgs: &OrganizationsClient{
			clientContext: ctx,
		},
		orgRepos: &OrgRepositoriesClient{
			clientContext: ctx,
		},
		userRepos: &UserRepositoriesClient{
			clientContext: ctx,
		},
	}
}

type clientContext struct {
	c                  *gitea.Client
	domain             string
	destructiveActions bool
}

// Client implements the gitprovider.Client interface.
var _ gitprovider.Client = &Client{}

// Client is an interface that allows talking to a Git provider.
type Client struct {
	*clientContext

	orgs      *OrganizationsClient
	orgRepos  *OrgRepositoriesClient
	userRepos *UserRepositoriesClient
}

// SupportedDomain returns the domain endpoint for this client, e.g. "gitea.com", "gitea.dev.com" or
// "my-custom-git-server.com:6443". This allows a higher-level user to know what Client to use for
// what endpoints.
// This field is set at client creation time, and can't be changed.
func (c *Client) SupportedDomain() string {
	return c.domain
}

// ProviderID returns the provider ID "gitea".
// This field is set at client creation time, and can't be changed.
func (c *Client) ProviderID() gitprovider.ProviderID {
	return ProviderID
}

// Raw returns the Gitea client (code.gitea.io/sdk/gitea *Client)
// used under the hood for accessing Gitea.
func (c *Client) Raw() interface{} {
	return c.c
}

// Organizations returns the OrganizationsClient handling sets of organizations.
func (c *Client) Organizations() gitprovider.OrganizationsClient {
	return c.orgs
}

// OrgRepositories returns the OrgRepositoriesClient handling sets of repositories in an organization.
func (c *Client) OrgRepositories() gitprovider.OrgRepositoriesClient {
	return c.orgRepos
}

// UserRepositories returns the UserRepositoriesClient handling sets of repositories for a user.
func (c *Client) UserRepositories() gitprovider.UserRepositoriesClient {
	return c.userRepos
}

// HasTokenPermission returns true if the given token has the given permissions.
func (c *Client) HasTokenPermission(ctx context.Context, permission gitprovider.TokenPermission) (bool, error) {
	return false, gitprovider.ErrNoProviderSupport
}
