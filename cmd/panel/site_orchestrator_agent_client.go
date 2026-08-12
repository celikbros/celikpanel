package main

import (
	"context"
	"errors"
)

var errPanelSiteAgentClientUnavailable = errors.New("guarded panel Agent client is unavailable")

// panelSiteAgentClient ensures the services-layer site orchestrator cannot
// bypass the panel's reviewed timeout and platform-capability policy.
type panelSiteAgentClient struct {
	panel *Panel
}

var _ interface {
	AuthorizeContext(context.Context, string, any) error
} = panelSiteAgentClient{}

func (client panelSiteAgentClient) CallContext(
	ctx context.Context,
	method string,
	args, reply any,
) error {
	if client.panel == nil {
		return errPanelSiteAgentClientUnavailable
	}
	return client.panel.callAgentContext(ctx, method, args, reply)
}

func (client panelSiteAgentClient) AuthorizeContext(
	ctx context.Context,
	method string,
	_ any,
) error {
	if client.panel == nil {
		return errPanelSiteAgentClientUnavailable
	}
	return client.panel.authorizeAgentRPCContext(ctx, method)
}
