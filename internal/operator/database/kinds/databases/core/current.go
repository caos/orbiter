package core

import (
	"crypto/rsa"
	"errors"
	"github.com/caos/orbos/internal/operator/core"
	"github.com/caos/orbos/pkg/kubernetes"
	"github.com/caos/orbos/pkg/tree"
)

const queriedName = "database"

type DatabaseCurrent interface {
	GetURL() string
	GetPort() string
	GetReadyQuery() core.EnsureFunc
	GetCertificateKey() *rsa.PrivateKey
	SetCertificateKey(*rsa.PrivateKey)
	GetCertificate() []byte
	SetCertificate([]byte)
	GetAddUserFunc() func(user string) (core.QueryFunc, error)
	GetDeleteUserFunc() func(user string) (core.DestroyFunc, error)
	GetListUsersFunc() func(k8sClient *kubernetes.Client) ([]string, error)
	GetListDatabasesFunc() func(k8sClient *kubernetes.Client) ([]string, error)
}

func ParseQueriedForDatabase(queried map[string]interface{}) (DatabaseCurrent, error) {
	queriedDB, ok := queried[queriedName]
	if !ok {
		return nil, errors.New("no current state for database found")
	}
	currentDBTree, ok := queriedDB.(*tree.Tree)
	if !ok {
		return nil, errors.New("current state does not fullfil interface")
	}
	currentDB, ok := currentDBTree.Parsed.(DatabaseCurrent)
	if !ok {
		return nil, errors.New("current state does not fullfil interface")
	}

	return currentDB, nil
}

func SetQueriedForDatabase(queried map[string]interface{}, databaseCurrent *tree.Tree) {
	queried[queriedName] = databaseCurrent
}

func SetQueriedForDatabaseDBList(queried map[string]interface{}, databases []string) {
	currentDBList := &CurrentDBList{
		Common: &tree.Common{
			Kind:    "DBList",
			Version: "V0",
		},
		Current: &DatabaseCurrentDBList{
			Databases: databases,
		},
	}
	currentDB := &tree.Tree{
		Parsed: currentDBList,
	}
	queried[queriedName] = currentDB
}
