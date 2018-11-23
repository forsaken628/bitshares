package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"html/template"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/denkhaus/gojson"
	"github.com/iuouiyiuty/bitshares/api"
	"github.com/iuouiyiuty/bitshares/gen/data"
	"github.com/iuouiyiuty/bitshares/tests"
	"github.com/iuouiyiuty/bitshares/types"
	"github.com/iuouiyiuty/bitshares/util"
	"github.com/juju/errors"
	"github.com/pquerna/ffjson/ffjson"
	"github.com/stretchr/objx"
	"gopkg.in/tomb.v2"

	// importing this initializes sample data fetching
	_ "github.com/iuouiyiuty/bitshares/gen/samples"
)

type Unmarshalable interface {
	UnmarshalJSON(input []byte) error
}

var (
	sampleDataTemplate *template.Template
	samplesDir         = "samples"
	operationsDir      = "operations"
	genChan            = make(chan GenData, 40)
	tb                 = tomb.Tomb{}

	// do not change order here
	knownTypes = []Unmarshalable{
		//&types.AccountOptions{},
		// &types.Asset{},
		&types.AssetAmount{},
		&types.AssetFeed{},
		// &types.AssetOptions{},
		&types.GrapheneID{},
		//&types.BitAssetDataOptions{},
		&types.Authority{},
		//&types.Memo{},
		&types.Price{},
		&types.PriceFeed{},
		//&types.Votes{},
		&types.Time{},
		//&types.PublicKey{},
		//&types.Account{},
	}
)

type GenData struct {
	Type types.OperationType
	Data objx.Map
}

func main() {

	defer close(genChan)

	api := api.New(tests.WsFullApiUrl, tests.RpcFullApiUrl)
	if err := api.Connect(); err != nil {
		handleError(errors.Annotate(err, "Connect"))
	}

	api.OnError(func(err error) {
		handleError(errors.Annotate(err, "OnError"))
	})

	//init templates
	tmpl, err := template.ParseFiles("templates/opsampledata.go.tmpl")
	if err != nil {
		handleError(errors.Annotate(err, "ParseFiles"))
	}
	sampleDataTemplate = tmpl

	// start generate goroutine
	tb.Go(func() error {
		return generate(genChan)
	})

	dataStore := NewOpDataStore()
	if err := dataStore.Init(data.OpSampleMap, genChan); err != nil {
		handleError(errors.Annotate(err, "init datastore"))
	}

	//TODO: save last scanned block and reapply
	block := uint64(20435987)

	fmt.Println("loop blocks")

	for tb.Alive() {
		resp, err := api.CallWsAPI(0, "get_block", block)
		if err != nil {
			handleError(errors.Annotate(err, "GetBlock"))
		}

		m := objx.New(resp)

		trxs := m.Get("transactions")
		// enumerate Transactions
		trxs.EachInter(func(_ int, trx interface{}) bool {
			ops := objx.New(trx).Get("operations")
			// enumerate Operations
			ops.EachInter(func(_ int, o interface{}) bool {
				op := o.([]interface{})
				opType := types.OperationType(types.Int8(op[0].(float64)))
				opData := objx.New(op[1])

				blob := NewOperationBlob(opData)
				ok, err := dataStore.Evaluate(opType, blob, block)
				if err != nil {
					handleError(errors.Annotate(err, "Evaluate"))
				}

				if ok && tb.Alive() {
					genChan <- GenData{
						Type: opType,
						Data: opData,
					}
				}

				return true
			})

			return true
		})

		block++
	}

	if err := tb.Err(); err != nil {
		handleError(errors.Annotate(err, "main"))
	}
}

func generate(ch chan GenData) error {
	for {
		select {
		case data := <-ch:
			if err := generateSampleData(data); err != nil {
				return errors.Annotate(err, "generateSampleData")
			}

			if err := generateOpData(data); err != nil {
				return errors.Annotate(err, "generateOpData")
			}

		case <-tb.Dying():
			return nil
		default:
		}
	}
}

func generateOpData(d GenData) error {
	s, err := data.GetSampleByType(d.Type)
	if err != nil {
		return errors.Annotate(err, "GetSampleByType")
	}

	sample, err := strconv.Unquote(s)
	if err != nil {
		return errors.Annotate(err, "Unquote")
	}

	//fmt.Printf("generate struct by sample %+v\n", sample)

	buf, err := gojson.GenerateWithTypeGuessing(
		strings.NewReader(sample),
		gojson.ParseJson, d.Type.OperationName(),
		"operations", []string{"json"}, true, true,
		guessStructType,
	)

	if err != nil {
		return errors.Annotate(err, "GenerateWithTypeGuessing")
	}

	fmt.Println("generated struct ", string(buf))
	return nil
}

func generateSampleData(d GenData) error {
	opName := d.Type.OperationName()

	sampleDataJSON, err := json.MarshalIndent(d.Data, "", "  ")
	if err != nil {
		return errors.Annotate(err, "MarshalIndent")
	}

	sampleData := fmt.Sprintf("`%s`", sampleDataJSON)

	//update sample map too
	data.OpSampleMap[d.Type] = sampleData

	buf := bytes.NewBuffer(nil)
	err = sampleDataTemplate.Execute(buf, struct {
		SampleDataOpType  string
		SampleData        interface{}
		SampleDataVarName string
	}{
		SampleDataOpType:  d.Type.String(),
		SampleData:        template.HTML(sampleData),
		SampleDataVarName: fmt.Sprintf("sampleData%s", opName),
	})

	if err != nil {
		return errors.Annotate(err, "Execute")
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return errors.Annotate(err, "Source")
	}

	fileName := strings.ToLower(fmt.Sprintf("%s/%s.go", samplesDir, opName))
	if err := ioutil.WriteFile(fileName, formatted, 0622); err != nil {
		return errors.Annotate(err, "WriteFile")
	}

	return nil
}

func handleError(err error) {
	fmt.Println("error: ", errors.ErrorStack(err))

	if tb.Alive() {
		//kill generator goroutine and wait
		tb.Kill(err)
		tb.Wait()
	}

	os.Exit(1)
}

func guessStructType(value interface{}, suggestedType string) (string, error) {
	//util.Dump("valueToGuess", value)
	//	util.DumpJSON("suggestedType", suggestedType)

	for _, t := range knownTypes {
		v, err := ffjson.Marshal(value)
		if err != nil {
			return "", errors.Annotate(err, "Marshal")
		}

		//make local copy of known type
		typ := t

		//	util.Dump("data", v)
		if err := typ.UnmarshalJSON(v); err == nil {
			// util.Dump("compare-1", typ)
			// util.Dump("compare-2", value)

			switch o := typ.(type) {
			case *types.GrapheneID:
				if value.(string) == o.String() {
					return "types.GrapheneID", nil
				}
			case *types.Time:
				if value.(string) == o.String() {
					return "types.Time", nil
				}
			// case *types.PublicKey:
			// 	if value.(string) == o.String() {
			// 		return "types.PublicKey", nil
			// 	}
			case *types.AccountOptions:
				if bytes.Equal(v, util.ToBytes(typ)) {
					return "types.AccountOptions", nil
				}
			case *types.AssetAmount:
				if bytes.Equal(v, util.ToBytes(typ)) {
					return "types.AssetAmount", nil
				}
			case *types.Authority:
				if bytes.Equal(v, util.ToBytes(typ)) {
					return "types.Authority", nil
				}
			case *types.Memo:
				if bytes.Equal(v, util.ToBytes(typ)) {
					return "types.Memo", nil
				}
			// case *types.Votes:
			// 	if bytes.Equal(v, util.ToBytes(typ)) {
			// 		return "types.Votes", nil
			// 	}
			case *types.Price:
				if bytes.Equal(v, util.ToBytes(typ)) {
					return "types.Price", nil
				}
			case *types.PriceFeed:
				if bytes.Equal(v, util.ToBytes(typ)) {
					return "types.PriceFeed", nil
				}
			case *types.AssetFeed:
				if bytes.Equal(v, util.ToBytes(typ)) {
					return "types.AssetFeed", nil
				}
			}
			// if val1, ok := value.(map[string]interface{}); ok {
			// 	val2 := util.ToMap(typ)

			// 	util.Dump("compare-1", val1)
			// 	util.Dump("compare-2", val2)

			// 	if reflect.DeepEqual(val1, val2) {
			// 		util.Dump("solved", typ)
			// 		return "lala", nil
			// 	}
			// }
			// name := reflect.ValueOf(typ).Type().Name()
			// util.Dump("solved", typ)
			// util.Dump("typeName", name)
			//o1 := util.ToMap(value)
		}
	}

	return suggestedType, nil
}
