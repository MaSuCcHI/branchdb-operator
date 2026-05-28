package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"errors"

	v1alpha1 "github.com/MaSuCcHI/branchdb-operator/api/v1alpha1"
	"github.com/MaSuCcHI/branchdb-operator/internal/interface/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mockK8sClient is a mock DatabaseBranchClient that returns configurable errors.
type mockK8sClient struct {
	createErr error
	getErr    error
	listErr   error
	deleteErr error
	// getCR is returned when Get is called and getErr is nil.
	getCR *v1alpha1.DatabaseBranch
	// listItems is returned when List is called and listErr is nil.
	listItems []v1alpha1.DatabaseBranch
}

func (m *mockK8sClient) Create(_ context.Context, _ client.Object, _ ...client.CreateOption) error {
	return m.createErr
}

func (m *mockK8sClient) Get(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
	if m.getErr != nil {
		return m.getErr
	}
	if m.getCR != nil {
		if cr, ok := obj.(*v1alpha1.DatabaseBranch); ok {
			*cr = *m.getCR
		}
	}
	return nil
}

func (m *mockK8sClient) List(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
	if m.listErr != nil {
		return m.listErr
	}
	if brList, ok := list.(*v1alpha1.DatabaseBranchList); ok {
		brList.Items = m.listItems
	}
	return nil
}

func (m *mockK8sClient) Delete(_ context.Context, _ client.Object, _ ...client.DeleteOption) error {
	return m.deleteErr
}

var errK8sFailed = errors.New("k8s API error")

func TestK8sGetBranches_listエラー時500を返す(t *testing.T) {
	mock := &mockK8sClient{listErr: errK8sFailed}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestK8sGetBranch_getエラー時500を返す(t *testing.T) {
	mock := &mockK8sClient{getErr: errK8sFailed}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/branches/feat-x", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestK8sDeleteBranch_getエラー時500を返す(t *testing.T) {
	mock := &mockK8sClient{getErr: errK8sFailed}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodDelete, "/branches/feat-x", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestK8sDeleteBranch_deleteエラー時500を返す(t *testing.T) {
	now := metav1.NewTime(time.Now())
	cr := &v1alpha1.DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "feat-del-err",
			Namespace:         "default",
			CreationTimestamp: now,
		},
	}
	mock := &mockK8sClient{getCR: cr, deleteErr: errK8sFailed}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	req := httptest.NewRequest(http.MethodDelete, "/branches/feat-del-err", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestK8sPostBranches_createエラー時500を返す(t *testing.T) {
	mock := &mockK8sClient{createErr: errK8sFailed}

	handler := api.NewK8sBranchHandler(mock, "branchdb.example.com")
	router := api.NewK8sRouter(handler)

	body := bytes.NewReader([]byte(`{"name": "feat-create-err"}`))
	req := httptest.NewRequest(http.MethodPost, "/branches", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", w.Code)
	}
}
