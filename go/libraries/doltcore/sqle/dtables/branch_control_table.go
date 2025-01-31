// Copyright 2022 Dolthub, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dtables

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/dolthub/go-mysql-server/sql"
	"github.com/dolthub/vitess/go/sqltypes"

	"github.com/dolthub/dolt/go/libraries/doltcore/branch_control"
	"github.com/dolthub/dolt/go/libraries/doltcore/sqle/index"
)

const (
	AccessTableName = "dolt_branch_control"
)

// PermissionsStrings is a slice of strings representing the available branch_control.branch_control.Permissions. The order of the
// strings should exactly match the order of the branch_control.Permissions according to their flag value.
var PermissionsStrings = []string{"admin", "write"}

// accessSchema is the schema for the "dolt_branch_control" table.
var accessSchema = sql.Schema{
	&sql.Column{
		Name:       "branch",
		Type:       sql.MustCreateString(sqltypes.VarChar, 16383, sql.Collation_utf8mb4_0900_ai_ci),
		Source:     AccessTableName,
		PrimaryKey: true,
	},
	&sql.Column{
		Name:       "user",
		Type:       sql.MustCreateString(sqltypes.VarChar, 16383, sql.Collation_utf8mb4_0900_bin),
		Source:     AccessTableName,
		PrimaryKey: true,
	},
	&sql.Column{
		Name:       "host",
		Type:       sql.MustCreateString(sqltypes.VarChar, 16383, sql.Collation_utf8mb4_0900_ai_ci),
		Source:     AccessTableName,
		PrimaryKey: true,
	},
	&sql.Column{
		Name:       "permissions",
		Type:       sql.MustCreateSetType(PermissionsStrings, sql.Collation_utf8mb4_0900_ai_ci),
		Source:     AccessTableName,
		PrimaryKey: false,
	},
}

// BranchControlTable provides a layer over the branch_control.Access structure, exposing it as a system table.
type BranchControlTable struct {
	*branch_control.Access
}

var _ sql.Table = BranchControlTable{}
var _ sql.InsertableTable = BranchControlTable{}
var _ sql.ReplaceableTable = BranchControlTable{}
var _ sql.UpdatableTable = BranchControlTable{}
var _ sql.DeletableTable = BranchControlTable{}
var _ sql.RowInserter = BranchControlTable{}
var _ sql.RowReplacer = BranchControlTable{}
var _ sql.RowUpdater = BranchControlTable{}
var _ sql.RowDeleter = BranchControlTable{}

// NewBranchControlTable returns a new BranchControlTable.
func NewBranchControlTable(access *branch_control.Access) BranchControlTable {
	return BranchControlTable{access}
}

// Name implements the interface sql.Table.
func (tbl BranchControlTable) Name() string {
	return AccessTableName
}

// String implements the interface sql.Table.
func (tbl BranchControlTable) String() string {
	return AccessTableName
}

// Schema implements the interface sql.Table.
func (tbl BranchControlTable) Schema() sql.Schema {
	return accessSchema
}

// Collation implements the interface sql.Table.
func (tbl BranchControlTable) Collation() sql.CollationID {
	return sql.Collation_Default
}

// Partitions implements the interface sql.Table.
func (tbl BranchControlTable) Partitions(context *sql.Context) (sql.PartitionIter, error) {
	return index.SinglePartitionIterFromNomsMap(nil), nil
}

// PartitionRows implements the interface sql.Table.
func (tbl BranchControlTable) PartitionRows(context *sql.Context, partition sql.Partition) (sql.RowIter, error) {
	tbl.RWMutex.RLock()
	defer tbl.RWMutex.RUnlock()

	rows := []sql.Row{{"%", tbl.SuperUser, tbl.SuperHost, uint64(branch_control.Permissions_Admin)}}
	for _, value := range tbl.Values {
		rows = append(rows, sql.Row{
			value.Branch,
			value.User,
			value.Host,
			uint64(value.Permissions),
		})
	}
	return sql.RowsToRowIter(rows...), nil
}

// Inserter implements the interface sql.InsertableTable.
func (tbl BranchControlTable) Inserter(context *sql.Context) sql.RowInserter {
	return tbl
}

// Replacer implements the interface sql.ReplaceableTable.
func (tbl BranchControlTable) Replacer(ctx *sql.Context) sql.RowReplacer {
	return tbl
}

// Updater implements the interface sql.UpdatableTable.
func (tbl BranchControlTable) Updater(ctx *sql.Context) sql.RowUpdater {
	return tbl
}

// Deleter implements the interface sql.DeletableTable.
func (tbl BranchControlTable) Deleter(context *sql.Context) sql.RowDeleter {
	return tbl
}

// StatementBegin implements the interface sql.TableEditor.
func (tbl BranchControlTable) StatementBegin(ctx *sql.Context) {
	//TODO: will use the binlog to implement
}

// DiscardChanges implements the interface sql.TableEditor.
func (tbl BranchControlTable) DiscardChanges(ctx *sql.Context, errorEncountered error) error {
	//TODO: will use the binlog to implement
	return nil
}

// StatementComplete implements the interface sql.TableEditor.
func (tbl BranchControlTable) StatementComplete(ctx *sql.Context) error {
	//TODO: will use the binlog to implement
	return nil
}

// Insert implements the interface sql.RowInserter.
func (tbl BranchControlTable) Insert(ctx *sql.Context, row sql.Row) error {
	tbl.RWMutex.Lock()
	defer tbl.RWMutex.Unlock()

	// Branch and Host are case-insensitive, while user is case-sensitive
	branch := strings.ToLower(branch_control.FoldExpression(row[0].(string)))
	user := branch_control.FoldExpression(row[1].(string))
	host := strings.ToLower(branch_control.FoldExpression(row[2].(string)))
	perms := branch_control.Permissions(row[3].(uint64))

	// Verify that the lengths of each expression fit within an uint16
	if len(branch) > math.MaxUint16 || len(user) > math.MaxUint16 || len(host) > math.MaxUint16 {
		return branch_control.ErrExpressionsTooLong.New(branch, user, host)
	}

	// A nil session means we're not in the SQL context, so we allow the insertion in such a case
	if branchAwareSession := branch_control.GetBranchAwareSession(ctx); branchAwareSession != nil {
		insertUser := branchAwareSession.GetUser()
		insertHost := branchAwareSession.GetHost()
		// As we've folded the branch expression, we can use it directly as though it were a normal branch name to
		// determine if the user attempting the insertion has permission to perform the insertion.
		_, modPerms := tbl.Match(branch, insertUser, insertHost)
		if modPerms&branch_control.Permissions_Admin != branch_control.Permissions_Admin {
			permStr, _ := accessSchema[3].Type.(sql.SetType).BitsToString(uint64(perms))
			return branch_control.ErrInsertingRow.New(insertUser, insertHost, branch, user, host, permStr)
		}
	}

	// We check if we're inserting a subset of an already-existing row. If we are, we deny the insertion as the existing
	// row will already match against ALL possible values for this row.
	_, modPerms := tbl.Match(branch, user, host)
	if modPerms&branch_control.Permissions_Admin == branch_control.Permissions_Admin {
		permBits := uint64(modPerms)
		permStr, _ := accessSchema[3].Type.(sql.SetType).BitsToString(permBits)
		return sql.NewUniqueKeyErr(
			fmt.Sprintf(`[%q, %q, %q, %q]`, branch, user, host, permStr),
			true,
			sql.Row{branch, user, host, permBits})
	}

	return tbl.insert(ctx, branch, user, host, perms)
}

// Update implements the interface sql.RowUpdater.
func (tbl BranchControlTable) Update(ctx *sql.Context, old sql.Row, new sql.Row) error {
	tbl.RWMutex.Lock()
	defer tbl.RWMutex.Unlock()

	// Branch and Host are case-insensitive, while user is case-sensitive
	oldBranch := strings.ToLower(branch_control.FoldExpression(old[0].(string)))
	oldUser := branch_control.FoldExpression(old[1].(string))
	oldHost := strings.ToLower(branch_control.FoldExpression(old[2].(string)))
	newBranch := strings.ToLower(branch_control.FoldExpression(new[0].(string)))
	newUser := branch_control.FoldExpression(new[1].(string))
	newHost := strings.ToLower(branch_control.FoldExpression(new[2].(string)))
	newPerms := branch_control.Permissions(new[3].(uint64))

	// Verify that the lengths of each expression fit within an uint16
	if len(newBranch) > math.MaxUint16 || len(newUser) > math.MaxUint16 || len(newHost) > math.MaxUint16 {
		return branch_control.ErrExpressionsTooLong.New(newBranch, newUser, newHost)
	}

	// If we're not updating the same row, then we pre-emptively check for a row violation
	if oldBranch != newBranch || oldUser != newUser || oldHost != newHost {
		if tblIndex := tbl.GetIndex(newBranch, newUser, newHost); tblIndex != -1 {
			permBits := uint64(tbl.Values[tblIndex].Permissions)
			permStr, _ := accessSchema[3].Type.(sql.SetType).BitsToString(permBits)
			return sql.NewUniqueKeyErr(
				fmt.Sprintf(`[%q, %q, %q, %q]`, newBranch, newUser, newHost, permStr),
				true,
				sql.Row{newBranch, newUser, newHost, permBits})
		}
	}

	// A nil session means we're not in the SQL context, so we'd allow the update in such a case
	if branchAwareSession := branch_control.GetBranchAwareSession(ctx); branchAwareSession != nil {
		insertUser := branchAwareSession.GetUser()
		insertHost := branchAwareSession.GetHost()
		// As we've folded the branch expression, we can use it directly as though it were a normal branch name to
		// determine if the user attempting the update has permission to perform the update on the old branch name.
		_, modPerms := tbl.Match(oldBranch, insertUser, insertHost)
		if modPerms&branch_control.Permissions_Admin != branch_control.Permissions_Admin {
			return branch_control.ErrUpdatingRow.New(insertUser, insertHost, oldBranch, oldUser, oldHost)
		}
		// Now we check if the user has permission use the new branch name
		_, modPerms = tbl.Match(newBranch, insertUser, insertHost)
		if modPerms&branch_control.Permissions_Admin != branch_control.Permissions_Admin {
			return branch_control.ErrUpdatingToRow.New(insertUser, insertHost, oldBranch, oldUser, oldHost, newBranch)
		}
	}

	// We check if we're updating to a subset of an already-existing row. If we are, we deny the update as the existing
	// row will already match against ALL possible values for this updated row.
	_, modPerms := tbl.Match(newBranch, newUser, newHost)
	if modPerms&branch_control.Permissions_Admin == branch_control.Permissions_Admin {
		permBits := uint64(modPerms)
		permStr, _ := accessSchema[3].Type.(sql.SetType).BitsToString(permBits)
		return sql.NewUniqueKeyErr(
			fmt.Sprintf(`[%q, %q, %q, %q]`, newBranch, newUser, newHost, permStr),
			true,
			sql.Row{newBranch, newUser, newHost, permBits})
	}

	if tblIndex := tbl.GetIndex(oldBranch, oldUser, oldHost); tblIndex != -1 {
		if err := tbl.delete(ctx, oldBranch, oldUser, oldHost); err != nil {
			return err
		}
	}
	return tbl.insert(ctx, newBranch, newUser, newHost, newPerms)
}

// Delete implements the interface sql.RowDeleter.
func (tbl BranchControlTable) Delete(ctx *sql.Context, row sql.Row) error {
	tbl.RWMutex.Lock()
	defer tbl.RWMutex.Unlock()

	// Branch and Host are case-insensitive, while user is case-sensitive
	branch := strings.ToLower(branch_control.FoldExpression(row[0].(string)))
	user := branch_control.FoldExpression(row[1].(string))
	host := strings.ToLower(branch_control.FoldExpression(row[2].(string)))

	// A nil session means we're not in the SQL context, so we allow the deletion in such a case
	if branchAwareSession := branch_control.GetBranchAwareSession(ctx); branchAwareSession != nil {
		insertUser := branchAwareSession.GetUser()
		insertHost := branchAwareSession.GetHost()
		// As we've folded the branch expression, we can use it directly as though it were a normal branch name to
		// determine if the user attempting the deletion has permission to perform the deletion.
		_, modPerms := tbl.Match(branch, insertUser, insertHost)
		if modPerms&branch_control.Permissions_Admin != branch_control.Permissions_Admin {
			return branch_control.ErrDeletingRow.New(insertUser, insertHost, branch, user, host)
		}
	}

	return tbl.delete(ctx, branch, user, host)
}

// Close implements the interface sql.Closer.
func (tbl BranchControlTable) Close(context *sql.Context) error {
	return branch_control.SaveData(context)
}

// insert adds the given branch, user, and host expression strings to the table. Assumes that the expressions have
// already been folded.
func (tbl BranchControlTable) insert(ctx context.Context, branch string, user string, host string, perms branch_control.Permissions) error {
	// If we already have this in the table, then we return a duplicate PK error
	if tblIndex := tbl.GetIndex(branch, user, host); tblIndex != -1 {
		permBits := uint64(tbl.Values[tblIndex].Permissions)
		permStr, _ := accessSchema[3].Type.(sql.SetType).BitsToString(permBits)
		return sql.NewUniqueKeyErr(
			fmt.Sprintf(`[%q, %q, %q, %q]`, branch, user, host, permStr),
			true,
			sql.Row{branch, user, host, permBits})
	}

	// Add the expressions to their respective slices
	branchExpr := branch_control.ParseExpression(branch, sql.Collation_utf8mb4_0900_ai_ci)
	userExpr := branch_control.ParseExpression(user, sql.Collation_utf8mb4_0900_bin)
	hostExpr := branch_control.ParseExpression(host, sql.Collation_utf8mb4_0900_ai_ci)
	nextIdx := uint32(len(tbl.Values))
	tbl.Branches = append(tbl.Branches, branch_control.MatchExpression{CollectionIndex: nextIdx, SortOrders: branchExpr})
	tbl.Users = append(tbl.Users, branch_control.MatchExpression{CollectionIndex: nextIdx, SortOrders: userExpr})
	tbl.Hosts = append(tbl.Hosts, branch_control.MatchExpression{CollectionIndex: nextIdx, SortOrders: hostExpr})
	tbl.Values = append(tbl.Values, branch_control.AccessValue{
		Branch:      branch,
		User:        user,
		Host:        host,
		Permissions: perms,
	})
	return nil
}

// delete removes the given branch, user, and host expression strings from the table. Assumes that the expressions have
// already been folded.
func (tbl BranchControlTable) delete(ctx context.Context, branch string, user string, host string) error {
	// If we don't have this in the table, then we just return
	tblIndex := tbl.GetIndex(branch, user, host)
	if tblIndex == -1 {
		return nil
	}

	endIndex := len(tbl.Values) - 1
	// Remove the matching row from all slices by first swapping with the last element
	tbl.Branches[tblIndex], tbl.Branches[endIndex] = tbl.Branches[endIndex], tbl.Branches[tblIndex]
	tbl.Users[tblIndex], tbl.Users[endIndex] = tbl.Users[endIndex], tbl.Users[tblIndex]
	tbl.Hosts[tblIndex], tbl.Hosts[endIndex] = tbl.Hosts[endIndex], tbl.Hosts[tblIndex]
	tbl.Values[tblIndex], tbl.Values[endIndex] = tbl.Values[endIndex], tbl.Values[tblIndex]
	// Then we remove the last element
	tbl.Branches = tbl.Branches[:endIndex]
	tbl.Users = tbl.Users[:endIndex]
	tbl.Hosts = tbl.Hosts[:endIndex]
	tbl.Values = tbl.Values[:endIndex]
	return nil
}
