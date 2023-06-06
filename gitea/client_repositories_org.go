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
	"errors"
	"fmt"

	"code.gitea.io/sdk/gitea"

	"github.com/fluxcd/go-git-providers/gitprovider"
)

// OrgRepositoriesClient implements the gitprovider.OrgRepositoriesClient interface.
var _ gitprovider.OrgRepositoriesClient = &OrgRepositoriesClient{}

var knownLicenseTemplateMap = map[string]string{
	"apache-2.0": "Apache-2.0",
	"mit":        "MIT",
	"gpl-3.0":    "GPL-3.0-only",
}

// OrgRepositoriesClient operates on repositories the user has access to.
type OrgRepositoriesClient struct {
	*clientContext
}

// Get returns the repository at the given path.
//
// ErrNotFound is returned if the resource does not exist.
func (c *OrgRepositoriesClient) Get(ctx context.Context, ref gitprovider.OrgRepositoryRef) (gitprovider.OrgRepository, error) {
	// Make sure the OrgRepositoryRef is valid
	if err := validateOrgRepositoryRef(ref, c.domain); err != nil {
		return nil, err
	}
	// GET /repos/{owner}/{repo}
	apiObj, err := getRepo(c.c, ref.GetIdentity(), ref.GetRepository())
	if err != nil {
		return nil, err
	}
	return newOrgRepository(c.clientContext, apiObj, ref), nil
}

// List all repositories in the given organization.
//
// List returns all available repositories, using multiple paginated requests if needed.
func (c *OrgRepositoriesClient) List(ctx context.Context, ref gitprovider.OrganizationRef) ([]gitprovider.OrgRepository, error) {
	// Make sure the OrganizationRef is valid
	if err := validateOrganizationRef(ref, c.domain); err != nil {
		return nil, err
	}

	// GET /orgs/{org}/repos
	apiObjs, err := c.listOrgRepos(ref.Organization)
	if err != nil {
		return nil, err
	}

	// Traverse the list, and return a list of OrgRepository objects
	repos := make([]gitprovider.OrgRepository, 0, len(apiObjs))
	for _, apiObj := range apiObjs {
		// apiObj is already validated at ListOrgRepos
		repos = append(repos, newOrgRepository(c.clientContext, apiObj, gitprovider.OrgRepositoryRef{
			OrganizationRef: ref,
			RepositoryName:  *&apiObj.Name,
		}))
	}
	return repos, nil
}

// Create creates a repository for the given organization, with the data and options.
//
// ErrAlreadyExists will be returned if the resource already exists.
func (c *OrgRepositoriesClient) Create(ctx context.Context, ref gitprovider.OrgRepositoryRef, req gitprovider.RepositoryInfo, opts ...gitprovider.RepositoryCreateOption) (gitprovider.OrgRepository, error) {
	// Make sure the RepositoryRef is valid
	if err := validateOrgRepositoryRef(ref, c.domain); err != nil {
		return nil, err
	}

	apiObj, err := createRepository(ctx, c.c, ref, ref.Organization, req, opts...)
	if err != nil {
		return nil, err
	}
	return newOrgRepository(c.clientContext, apiObj, ref), nil
}

// Reconcile makes sure the given desired state (req) becomes the actual state in the backing Git provider.
//
// If req doesn't exist under the hood, it is created (actionTaken == true).
// If req doesn't equal the actual state, the resource will be updated (actionTaken == true).
// If req is already the actual state, this is a no-op (actionTaken == false).
func (c *OrgRepositoriesClient) Reconcile(ctx context.Context, ref gitprovider.OrgRepositoryRef, req gitprovider.RepositoryInfo, opts ...gitprovider.RepositoryReconcileOption) (gitprovider.OrgRepository, bool, error) {
	// First thing, validate and default the request to ensure a valid and fully-populated object
	// (to minimize any possible diffs between desired and actual state)
	if err := gitprovider.ValidateAndDefaultInfo(&req); err != nil {
		return nil, false, err
	}

	actual, err := c.Get(ctx, ref)
	if err != nil {
		// Create if not found
		if errors.Is(err, gitprovider.ErrNotFound) {
			resp, err := c.Create(ctx, ref, req, toCreateOpts(opts...)...)
			return resp, true, err
		}

		// Unexpected path, Get should succeed or return NotFound
		return nil, false, err
	}
	// Run generic reconciliation
	actionTaken, err := reconcileRepository(ctx, actual, req)
	return actual, actionTaken, err
}

// getRepo returns the repository of the given owner by name.
func getRepo(c *gitea.Client, owner, repo string) (*gitea.Repository, error) {
	apiObj, res, err := c.GetRepo(owner, repo)
	return validateRepositoryAPIResp(apiObj, res, err)
}

// listOrgRepos returns all repositories of the given organization the user has access to.
func (c *OrgRepositoriesClient) listOrgRepos(org string) ([]*gitea.Repository, error) {
	opts := gitea.ListOrgReposOptions{}
	apiObjs := []*gitea.Repository{}

	err := allPages(&opts.ListOptions, func() (*gitea.Response, error) {
		// GET /orgs/{org}/repos
		pageObjs, resp, listErr := c.c.ListOrgRepos(org, opts)
		if len(pageObjs) > 0 {
			apiObjs = append(apiObjs, pageObjs...)
			return resp, listErr
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	return validateRepositoryObjects(apiObjs)
}

func createRepository(ctx context.Context, c *gitea.Client, ref gitprovider.RepositoryRef, orgName string, req gitprovider.RepositoryInfo, opts ...gitprovider.RepositoryCreateOption) (*gitea.Repository, error) {
	// First thing, validate and default the request to ensure a valid and fully-populated object
	// (to minimize any possible diffs between desired and actual state)
	if err := gitprovider.ValidateAndDefaultInfo(&req); err != nil {
		return nil, err
	}

	// Assemble the options struct based on the given options
	o, err := gitprovider.MakeRepositoryCreateOptions(opts...)
	if err != nil {
		return nil, err
	}

	// Convert to the API object and apply the options
	apiOpts := repositoryToAPI(&req, ref)
	if o.AutoInit != nil {
		apiOpts.AutoInit = *o.AutoInit
	}
	if o.LicenseTemplate != nil {
		apiOpts.License = knownLicenseTemplateMap[string(*o.LicenseTemplate)]
	}

	return createRepo(c, orgName, apiOpts)
}

func createRepo(c *gitea.Client, orgName string, apiOpts gitea.CreateRepoOption) (*gitea.Repository, error) {
	if orgName != "" {
		apiObj, res, err := c.CreateOrgRepo(orgName, apiOpts)
		return validateRepositoryAPIResp(apiObj, res, err)
	}
	apiObj, res, err := c.CreateRepo(apiOpts)
	return validateRepositoryAPIResp(apiObj, res, err)
}

// updateRepo updates the given repository.
func updateRepo(c *gitea.Client, owner, repo string, req *gitea.EditRepoOption) (*gitea.Repository, error) {
	apiObj, res, err := c.EditRepo(owner, repo, *req)
	return validateRepositoryAPIResp(apiObj, res, err)
}

// deleteRepo deletes the given repository.
func deleteRepo(c *gitea.Client, owner, repo string, destructiveActions bool) error {
	// Don't allow deleting repositories if the user didn't explicitly allow dangerous API calls.
	if !destructiveActions {
		return fmt.Errorf("cannot delete repository: %w", gitprovider.ErrDestructiveCallDisallowed)
	}
	resp, err := c.DeleteRepo(owner, repo)
	return handleHTTPError(resp, err)
}

func reconcileRepository(ctx context.Context, actual gitprovider.UserRepository, req gitprovider.RepositoryInfo) (bool, error) {
	// If the desired matches the actual state, just return the actual state
	if req.Equals(actual.Get()) {
		return false, nil
	}
	// Populate the desired state to the current-actual object
	if err := actual.Set(req); err != nil {
		return false, err
	}
	// Apply the desired state by running Update
	return true, actual.Update(ctx)
}

func toCreateOpts(opts ...gitprovider.RepositoryReconcileOption) []gitprovider.RepositoryCreateOption {
	// Convert RepositoryReconcileOption => RepositoryCreateOption
	createOpts := make([]gitprovider.RepositoryCreateOption, 0, len(opts))
	for _, opt := range opts {
		createOpts = append(createOpts, opt)
	}
	return createOpts
}

func validateRepositoryAPIResp(apiObj *gitea.Repository, res *gitea.Response, err error) (*gitea.Repository, error) {
	// If the response contained an error, return
	if err != nil {
		return nil, handleHTTPError(res, err)
	}
	// Make sure apiObj is valid
	if err := validateRepositoryAPI(apiObj); err != nil {
		return nil, err
	}
	return apiObj, nil
}

func validateRepositoryObjects(apiObjs []*gitea.Repository) ([]*gitea.Repository, error) {
	for _, apiObj := range apiObjs {
		// Make sure apiObj is valid
		if err := validateRepositoryAPI(apiObj); err != nil {
			return nil, err
		}
	}
	return apiObjs, nil
}
