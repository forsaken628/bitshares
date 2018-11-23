//This file is generated by btsgen. DO NOT EDIT.
//operation sample data for OperationTypeWithdrawPermissionDelete

package samples

import (
	"github.com/iuouiyiuty/bitshares/gen/data"
	"github.com/iuouiyiuty/bitshares/types"
)

func init() {
	data.OpSampleMap[types.OperationTypeWithdrawPermissionDelete] =
		sampleDataWithdrawPermissionDeleteOperation
}

var sampleDataWithdrawPermissionDeleteOperation = `{
  "authorized_account": "1.2.492523",
  "fee": {
    "amount": 0,
    "asset_id": "1.3.0"
  },
  "withdraw_from_account": "1.2.21561",
  "withdrawal_permission": "1.12.2"
}`

//end of file
