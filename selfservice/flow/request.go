// Copyright © 2023 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	_ "embed"
	"net/http"
	"strings"

	"github.com/ory/kratos/driver/config"
	"github.com/ory/kratos/identity"
	"github.com/ory/kratos/selfservice/strategy"
	"github.com/ory/x/decoderx"

	"github.com/pkg/errors"

	"github.com/ory/herodot"
	"github.com/ory/kratos/x"
	"github.com/ory/nosurf"
)

//go:embed .schema/method.schema.json
var methodSchema []byte

var ErrOriginHeaderNeedsBrowserFlow = herodot.ErrBadRequest.
	WithReasonf(`The HTTP Request Header included the "Origin" key, indicating that this request was made as part of an AJAX request in a Browser. The flow however was initiated as an API request. To prevent potential misuse and mitigate several attack vectors including CSRF, the request has been blocked. Please consult the documentation.`)

var ErrCookieHeaderNeedsBrowserFlow = herodot.ErrBadRequest.
	WithReasonf(`The HTTP Request Header included the "Cookie" key, indicating that this request was made by a Browser. The flow however was initiated as an API request. To prevent potential misuse and mitigate several attack vectors including CSRF, the request has been blocked. Please consult the documentation.`)

func EnsureCSRF(
	reg config.Provider,
	r *http.Request,
	flowType Type,
	disableAPIFlowEnforcement bool,
	generator func(r *http.Request) string,
	actual string,
) error {
	switch flowType {
	case TypeAPI:
		if disableAPIFlowEnforcement {
			return nil
		}

		// API Based flows to not require anti-CSRF tokens because we can not leverage a session, making this
		// endpoint pointless.

		// Let's ensure that no-one mistakenly makes an AJAX request using the API flow.
		if r.Header.Get("Origin") != "" {
			return errors.WithStack(ErrOriginHeaderNeedsBrowserFlow)
		}

		// Workaround for Cloudflare setting cookies that we can't control.
		var cookies []string
		for _, c := range r.Cookies() {
			if !strings.HasPrefix(c.Name, "__cf") {
				cookies = append(cookies, c.Name)
			}
		}

		if len(cookies) > 0 {
			return errors.WithStack(ErrCookieHeaderNeedsBrowserFlow.WithDetail("found cookies", cookies))
		}

		return nil
	default:
		if !nosurf.VerifyToken(generator(r), actual) {
			return errors.WithStack(x.CSRFErrorReason(r, reg))
		}
	}

	return nil
}

var dec = decoderx.NewHTTP()

func MethodEnabledAndAllowedFromRequest(r *http.Request, flow FlowName, expected string, d interface {
	config.Provider
},
) error {
	var method struct {
		Method string `json:"method" form:"method"`
	}

	compiler, err := decoderx.HTTPRawJSONSchemaCompiler(methodSchema)
	if err != nil {
		return errors.WithStack(err)
	}

	if err := dec.Decode(r, &method, compiler,
		decoderx.HTTPKeepRequestBody(true),
		decoderx.HTTPDecoderAllowedMethods("POST", "PUT", "PATCH", "GET"),
		decoderx.HTTPDecoderSetValidatePayloads(false),
		decoderx.HTTPDecoderJSONFollowsFormFormat()); err != nil {
		return errors.WithStack(err)
	}

	return MethodEnabledAndAllowed(r.Context(), flow, expected, method.Method, d)
}

func MethodEnabledAndAllowed(ctx context.Context, flowName FlowName, expected, actual string, d config.Provider) error {
	if actual != expected {
		return errors.WithStack(ErrStrategyNotResponsible)
	}

	var ok bool
	if strings.EqualFold(actual, identity.CredentialsTypeCodeAuth.String()) {
		switch flowName {
		case RegistrationFlow, LoginFlow:
			ok = d.Config().SelfServiceCodeStrategy(ctx).PasswordlessEnabled
		case VerificationFlow, RecoveryFlow:
			ok = d.Config().SelfServiceCodeStrategy(ctx).Enabled
		}
	} else {
		ok = d.Config().SelfServiceStrategy(ctx, expected).Enabled
	}

	if !ok {
		return errors.WithStack(herodot.ErrNotFound.WithReason(strategy.EndpointDisabledMessage))
	}

	return nil
}
