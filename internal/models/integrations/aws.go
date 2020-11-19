package integrations

import (
	"gorm.io/gorm"

	"github.com/aws/aws-sdk-go/aws"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	token "sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

// AWSIntegration is an auth mechanism that uses a AWS IAM user to
// authenticate
type AWSIntegration struct {
	gorm.Model

	// The id of the user that linked this auth mechanism
	UserID uint `json:"user_id"`

	// The project that this integration belongs to
	ProjectID uint `json:"project_id"`

	// The AWS entity this is linked to (individual or organization)
	AWSEntityID string `json:"aws-entity-id"`

	// The AWS caller identity (ARN) which linked this service
	AWSCallerID string `json:"aws-caller-id"`

	// ------------------------------------------------------------------
	// All fields encrypted before storage.
	// ------------------------------------------------------------------

	// The AWS cluster ID
	// See https://github.com/kubernetes-sigs/aws-iam-authenticator#what-is-a-cluster-id
	AWSClusterID []byte `json:"aws_cluster_id"`

	// The AWS access key for this IAM user
	AWSAccessKeyID []byte `json:"aws_access_key_id"`

	// The AWS secret key for this IAM user
	AWSSecretAccessKey []byte `json:"aws_secret_access_key"`

	// An optional session token, if the user is assuming a role
	AWSSessionToken []byte `json:"aws_session_token"`
}

// AWSIntegrationExternal is a AWSIntegration to be shared over REST
type AWSIntegrationExternal struct {
	ID uint `json:"id"`

	// The id of the user that linked this auth mechanism
	UserID uint `json:"user_id"`

	// The project that this integration belongs to
	ProjectID uint `json:"project_id"`

	// The AWS entity this is linked to (individual or organization)
	AWSEntityID string `json:"aws-entity-id"`

	// The AWS caller identity (ARN) which linked this service
	AWSCallerID string `json:"aws-caller-id"`
}

// Externalize generates an external KubeIntegration to be shared over REST
func (a *AWSIntegration) Externalize() *AWSIntegrationExternal {
	return &AWSIntegrationExternal{
		ID:          a.ID,
		UserID:      a.UserID,
		ProjectID:   a.ProjectID,
		AWSEntityID: a.AWSEntityID,
		AWSCallerID: a.AWSCallerID,
	}
}

// GetBearerToken retrieves a bearer token for an AWS account
func (a *AWSIntegration) GetBearerToken(
	getTokenCache GetTokenCacheFunc,
	setTokenCache SetTokenCacheFunc,
) (string, error) {
	cache, err := getTokenCache()

	// check the token cache for a non-expired token
	if tok := cache.Token; err == nil && !cache.IsExpired() && len(tok) > 0 {
		return string(tok), nil
	}

	generator, err := token.NewGenerator(false, false)

	if err != nil {
		return "", err
	}

	sess, err := session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config: aws.Config{
			Credentials: credentials.NewStaticCredentials(
				string(a.AWSAccessKeyID),
				string(a.AWSSecretAccessKey),
				string(a.AWSSessionToken),
			),
		},
	})

	if err != nil {
		return "", err
	}

	tok, err := generator.GetWithOptions(&token.GetTokenOptions{
		Session:   sess,
		ClusterID: string(a.AWSClusterID),
	})

	if err != nil {
		return "", err
	}

	setTokenCache(tok.Token, tok.Expiration)

	return tok.Token, nil
}
