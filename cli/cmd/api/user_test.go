package api_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/porter-dev/porter/cli/cmd/docker"

	"github.com/porter-dev/porter/cli/cmd/api"
)

const baseURL string = "http://localhost:10000/api"

func TestMain(m *testing.M) {
	err := startPorterServerWithDocker("user", 10000, docker.SQLite)

	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	stopPorterServerWithDocker("user")

	os.Exit(code)
}

func initUser(email string, client *api.Client, t *testing.T) *api.CreateUserResponse {
	t.Helper()

	resp, err := client.CreateUser(context.Background(), &api.CreateUserRequest{
		Email:    email,
		Password: "hello1234",
	})

	if err != nil {
		t.Fatalf("%v\n", err)
	}

	return resp
}

func TestLogin(t *testing.T) {
	email := "login_test@example.com"
	client := api.NewClient(baseURL, "cookie_login_test.json")
	user := initUser(email, client, t)

	resp, err := client.Login(context.Background(), &api.LoginRequest{
		Email:    user.Email,
		Password: "hello1234",
	})

	if err != nil {
		t.Fatalf("%v\n", err)
	}

	if resp.Email != user.Email {
		t.Errorf("incorrect email: expected %s, got %s\n", user.Email, resp.Email)
	}
}

func TestLogout(t *testing.T) {
	email := "logout_test@example.com"
	client := api.NewClient(baseURL, "cookie_logout_test.json")
	user := initUser(email, client, t)

	client.Login(context.Background(), &api.LoginRequest{
		Email:    user.Email,
		Password: "hello1234",
	})

	err := client.Logout(context.Background())

	if err != nil {
		t.Fatalf("%v\n", err)
	}

	// try to get the user and ensure 403
	_, err = client.AuthCheck(context.Background())

	if err != nil && !strings.Contains(err.Error(), "403") {
		t.Fatalf("%v\n", err)
	}
}

func TestAuthCheck(t *testing.T) {
	email := "auth_check_test@example.com"
	client := api.NewClient(baseURL, "cookie_auth_check_test.json")
	user := initUser(email, client, t)
	client.Login(context.Background(), &api.LoginRequest{
		Email:    user.Email,
		Password: "hello1234",
	})

	resp, err := client.AuthCheck(context.Background())

	if err != nil {
		t.Fatalf("%v\n", err)
	}

	if resp.Email != user.Email {
		t.Errorf("incorrect email: expected %s, got %s\n", user.Email, resp.Email)
	}
}

func TestGetUser(t *testing.T) {
	email := "get_user_test@example.com"
	client := api.NewClient(baseURL, "cookie_get_user_test.json")
	user := initUser(email, client, t)

	resp, err := client.GetUser(context.Background(), user.ID)

	if err != nil {
		t.Fatalf("%v\n", err)
	}

	if resp.Email != user.Email {
		t.Errorf("incorrect email: expected %s, got %s\n", user.Email, resp.Email)
	}
}

func TestDeleteUser(t *testing.T) {
	email := "delete_user_test@example.com"
	client := api.NewClient(baseURL, "cookie_delete_user_test.json")
	user := initUser(email, client, t)

	err := client.DeleteUser(context.Background(), user.ID, &api.DeleteUserRequest{
		Password: "hello1234",
	})

	if err != nil {
		t.Fatalf("%v\n", err)
	}

	_, err = client.GetUser(context.Background(), user.ID)

	if err != nil && !strings.Contains(err.Error(), "could not find requested object") {
		t.Fatalf("%v\n", err)
	}
}
