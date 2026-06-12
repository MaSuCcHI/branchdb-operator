package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BranchPhase はデータベースブランチのライフサイクルフェーズを表す。
type BranchPhase string

const (
	// FinalizerName はブランチ削除時のクリーンアップを保証する finalizer 名。
	FinalizerName = "branchdb.io/cleanup"

	// BranchPhasePending はブランチがスケジュール待ちの状態を表す。
	BranchPhasePending BranchPhase = "Pending"
	// BranchPhaseCreating はブランチが作成中の状態を表す。
	BranchPhaseCreating BranchPhase = "Creating"
	// BranchPhaseReady はブランチが使用可能な状態を表す。
	BranchPhaseReady BranchPhase = "Ready"
	// BranchPhaseError はブランチがエラー状態であることを表す。
	BranchPhaseError BranchPhase = "Error"
	// BranchPhaseDeleting はブランチが削除中の状態を表す。
	BranchPhaseDeleting BranchPhase = "Deleting"
)

// DatabaseBranchSpec は DatabaseBranch の望ましい状態を定義する。
type DatabaseBranchSpec struct {
	// SnapshotRef はブランチの元となるスナップショット名を指定する。
	// +kubebuilder:validation:Required
	SnapshotRef string `json:"snapshotRef"`

	// TTLHours はブランチの有効期間（時間）を指定する。0 は無期限を意味する。
	// +optional
	TTLHours int `json:"ttlHours,omitempty"`

	// DatabaseType は起動するデータベースの種類を指定する。
	// 対応値: mysql（デフォルト）, postgres, redis
	// +optional
	// +kubebuilder:default=mysql
	DatabaseType string `json:"databaseType,omitempty"`

	// DatabaseVersion はコンテナイメージのタグを上書きする。
	// 省略時は Operator のデフォルト（mysql:8.0, postgres:16, redis:7）を使用する。
	// +optional
	DatabaseVersion string `json:"databaseVersion,omitempty"`
}

// DatabaseBranchStatus は DatabaseBranch の観測された状態を定義する。
type DatabaseBranchStatus struct {
	// Phase はブランチの現在のライフサイクルフェーズを示す。
	// +optional
	Phase BranchPhase `json:"phase,omitempty"`

	// ClusterHost はクラスター内からアクセスするためのホスト名を示す。
	// +optional
	ClusterHost string `json:"clusterHost,omitempty"`

	// ClusterPort はクラスター内からアクセスするためのポート番号を示す。
	// +optional
	ClusterPort int `json:"clusterPort,omitempty"`

	// ExternalHost はクラスター外からアクセスするためのホスト名を示す。
	// +optional
	ExternalHost string `json:"externalHost,omitempty"`

	// ExternalPort はクラスター外からアクセスするためのポート番号を示す。
	// +optional
	ExternalPort int `json:"externalPort,omitempty"`

	// Message は現在の状態に関するメッセージを示す。
	// +optional
	Message string `json:"message,omitempty"`

	// ExpiresAt はブランチの有効期限を示す。
	// +optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// CredentialSecret はブランチ固有のパスワードを保存する K8s Secret 名を示す。
	// ZFSDB_BRANCH_AUTH=generated のときのみ設定される。空の場合は無認証。
	// Secret の内容を直接 status に含めることなく、参照のみを保存する。
	// +optional
	CredentialSecret string `json:"credentialSecret,omitempty"`
}

// DatabaseBranch は BranchDB が管理するデータベースブランチを表す。
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="ExternalPort",type="integer",JSONPath=".status.externalPort"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type DatabaseBranch struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabaseBranchSpec   `json:"spec,omitempty"`
	Status DatabaseBranchStatus `json:"status,omitempty"`
}

// DatabaseBranchList は DatabaseBranch のリストを表す。
//
// +kubebuilder:object:root=true
type DatabaseBranchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseBranch `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseBranch{}, &DatabaseBranchList{})
}
