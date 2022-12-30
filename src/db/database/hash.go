package database

import (
	"github.com/shopspring/decimal"
	"strconv"
	"strings"
	"traitor/db/interface/database"
	"traitor/db/interface/redis"
	"traitor/db/protocol"
	"traitor/db/struct/dict"
	utils "traitor/db/util"
)

func (db *DB) getAsDict(key string) (dict.Dict, protocol.ErrorReply) {
	entity, exists := db.GetEntity(key)
	if !exists {
		return nil, nil
	}
	d, ok := entity.Data.(dict.Dict)
	if ok == false {
		return nil, &protocol.WrongTypeErrReply{}
	}
	return d, nil
}

func (db *DB) getOrInitDict(key string) (d dict.Dict, init bool, errReply protocol.ErrorReply) {
	d, errReply = db.getAsDict(key)
	if errReply != nil {
		return nil, false, errReply
	}
	init = false
	if d == nil {
		d = dict.MakeSimple()
		db.PutEntity(key, &database.DataEntity{
			Data: d,
		})
		init = true
	}
	return d, init, nil
}

func execHSet(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	field := string(args[1])
	value := string(args[2])

	d, _, errReply := db.getOrInitDict(key)
	if errReply != nil {
		return errReply
	}
	var res = d.Put(field, value)
	db.addAof(utils.ToCmdLine3("hset", args...))
	return protocol.MakeIntReply(int64(res))
}

func undoHSet(db *DB, args [][]byte) []CmdLine {
	key := string(args[0])
	field := string(args[1])
	return rollbackHashFields(db, key, field)
}

// execHSetNX sets field in hash table only if field not exists
func execHSetNX(db *DB, args [][]byte) redis.Reply {
	// parse args
	key := string(args[0])
	field := string(args[1])
	value := args[2]

	d, _, errReply := db.getOrInitDict(key)
	if errReply != nil {
		return errReply
	}

	result := d.PutIfAbsent(field, value)
	if result > 0 {
		db.addAof(utils.ToCmdLine3("hsetnx", args...))

	}
	return protocol.MakeIntReply(int64(result))
}

// execHGet gets field value of hash table
func execHGet(db *DB, args [][]byte) redis.Reply {
	// parse args
	key := string(args[0])
	field := string(args[1])

	// get entity
	d, errReply := db.getAsDict(key)
	if errReply != nil {
		return errReply
	}
	if d == nil {
		return &protocol.NullBulkReply{}
	}

	raw, exists := d.Get(field)
	if !exists {
		return &protocol.NullBulkReply{}
	}
	value, _ := raw.([]byte)
	return protocol.MakeBulkReply(value)
}

// execHExists checks if a hash field exists
func execHExists(db *DB, args [][]byte) redis.Reply {
	// parse args
	key := string(args[0])
	field := string(args[1])

	// get entity
	d, errReply := db.getAsDict(key)
	if errReply != nil {
		return errReply
	}
	if d == nil {
		return protocol.MakeIntReply(0)
	}

	_, exists := d.Get(field)
	if exists {
		return protocol.MakeIntReply(1)
	}
	return protocol.MakeIntReply(0)
}

// execHDel deletes a hash field
func execHDel(db *DB, args [][]byte) redis.Reply {
	// parse args
	key := string(args[0])
	fields := make([]string, len(args)-1)
	fieldArgs := args[1:]
	for i, v := range fieldArgs {
		fields[i] = string(v)
	}

	// get entity
	d, errReply := db.getAsDict(key)
	if errReply != nil {
		return errReply
	}
	if d == nil {
		return protocol.MakeIntReply(0)
	}

	deleted := 0
	for _, field := range fields {
		result := d.Remove(field)
		deleted += result
	}
	if d.Len() == 0 {
		db.Remove(key)
	}
	if deleted > 0 {
		db.addAof(utils.ToCmdLine3("hdel", args...))
	}

	return protocol.MakeIntReply(int64(deleted))
}

func undoHDel(db *DB, args [][]byte) []CmdLine {
	key := string(args[0])
	fields := make([]string, len(args)-1)
	fieldArgs := args[1:]
	for i, v := range fieldArgs {
		fields[i] = string(v)
	}
	return rollbackHashFields(db, key, fields...)
}

// execHLen gets number of fields in hash table
func execHLen(db *DB, args [][]byte) redis.Reply {
	// parse args
	key := string(args[0])

	d, errReply := db.getAsDict(key)
	if errReply != nil {
		return errReply
	}
	if d == nil {
		return protocol.MakeIntReply(0)
	}
	return protocol.MakeIntReply(int64(d.Len()))
}

// execHStrLen Returns the string length of the value associated with field in the hash stored at key.
// If the key or the field do not exist, 0 is returned.
func execHStrLen(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	field := string(args[1])

	d, errReply := db.getAsDict(key)
	if errReply != nil {
		return errReply
	}
	if d == nil {
		return protocol.MakeIntReply(0)
	}

	raw, exists := d.Get(field)
	if exists {
		value, _ := raw.([]byte)
		return protocol.MakeIntReply(int64(len(value)))
	}
	return protocol.MakeIntReply(0)
}

// execHMSet sets multi fields in hash table
func execHMSet(db *DB, args [][]byte) redis.Reply {
	// parse args
	if len(args)%2 != 1 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])
	size := (len(args) - 1) / 2
	fields := make([]string, size)
	values := make([][]byte, size)
	for i := 0; i < size; i++ {
		fields[i] = string(args[2*i+1])
		values[i] = args[2*i+2]
	}

	// get or init entity
	d, _, errReply := db.getOrInitDict(key)
	if errReply != nil {
		return errReply
	}

	// put data
	for i, field := range fields {
		value := values[i]
		d.Put(field, value)
	}
	db.addAof(utils.ToCmdLine3("hmset", args...))
	return protocol.MakeOkReply()
}
func undoHMSet(db *DB, args [][]byte) []CmdLine {
	key := string(args[0])
	size := (len(args) - 1) / 2
	fields := make([]string, size)
	for i := 0; i < size; i++ {
		fields[i] = string(args[2*i+1])
	}
	return rollbackHashFields(db, key, fields...)
}

// execHMGet gets multi fields in hash table
func execHMGet(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	size := len(args) - 1
	fields := make([]string, size)
	for i := 0; i < size; i++ {
		fields[i] = string(args[i+1])
	}

	// get entity
	result := make([][]byte, size)
	d, errReply := db.getAsDict(key)
	if errReply != nil {
		return errReply
	}
	if d == nil {
		return protocol.MakeMultiBulkReply(result)
	}

	for i, field := range fields {
		value, ok := d.Get(field)
		if !ok {
			result[i] = nil
		} else {
			bytes, _ := value.([]byte)
			result[i] = bytes
		}
	}
	return protocol.MakeMultiBulkReply(result)
}

// execHKeys gets all field names in hash table
func execHKeys(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	d, errReply := db.getAsDict(key)
	if errReply != nil {
		return errReply
	}
	if d == nil {
		return &protocol.EmptyMultiBulkReply{}
	}

	fields := make([][]byte, d.Len())
	i := 0
	d.ForEach(func(key string, val interface{}) bool {
		fields[i] = []byte(key)
		i++
		return true
	})
	return protocol.MakeMultiBulkReply(fields[:i])
}

// execHVals gets all field value in hash table
func execHVals(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	// get entity
	d, errReply := db.getAsDict(key)
	if errReply != nil {
		return errReply
	}
	if d == nil {
		return &protocol.EmptyMultiBulkReply{}
	}

	values := make([][]byte, d.Len())
	i := 0
	d.ForEach(func(key string, val interface{}) bool {
		values[i], _ = val.([]byte)
		i++
		return true
	})
	return protocol.MakeMultiBulkReply(values[:i])
}

// execHGetAll gets all key-value entries in hash table
func execHGetAll(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])

	// get entity
	d, errReply := db.getAsDict(key)
	if errReply != nil {
		return errReply
	}
	if d == nil {
		return &protocol.EmptyMultiBulkReply{}
	}

	size := d.Len()
	result := make([][]byte, size*2)
	i := 0
	d.ForEach(func(key string, val interface{}) bool {
		result[i] = []byte(key)
		i++
		result[i], _ = val.([]byte)
		i++
		return true
	})
	return protocol.MakeMultiBulkReply(result[:i])
}

func execHIncrBy(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	field := string(args[1])
	rawDelta := string(args[2])
	delta, err := strconv.ParseInt(rawDelta, 10, 64)
	if err != nil {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}

	d, _, errReply := db.getOrInitDict(key)
	if errReply != nil {
		return errReply
	}

	value, exists := d.Get(field)
	if !exists {
		d.Put(field, args[2])
		db.addAof(utils.ToCmdLine3("hincrby", args...))
		return protocol.MakeBulkReply(args[2])
	}
	val, err := strconv.ParseInt(string(value.([]byte)), 10, 64)
	if err != nil {
		return protocol.MakeErrReply("ERR hash value is not an integer")
	}
	val += delta
	bytes := []byte(strconv.FormatInt(val, 10))
	d.Put(field, bytes)
	db.addAof(utils.ToCmdLine3("hincrby", args...))
	return protocol.MakeBulkReply(bytes)
}

func undoHIncr(db *DB, args [][]byte) []CmdLine {
	key := string(args[0])
	field := string(args[1])
	return rollbackHashFields(db, key, field)
}

// execHIncrByFloat increments the float value of a hash field by the given number
func execHIncrByFloat(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	field := string(args[1])
	rawDelta := string(args[2])
	delta, err := decimal.NewFromString(rawDelta)
	if err != nil {
		return protocol.MakeErrReply("ERR value is not a valid float")
	}

	// get or init entity
	d, _, errReply := db.getOrInitDict(key)
	if errReply != nil {
		return errReply
	}

	value, exists := d.Get(field)
	if !exists {
		d.Put(field, args[2])
		return protocol.MakeBulkReply(args[2])
	}
	val, err := decimal.NewFromString(string(value.([]byte)))
	if err != nil {
		return protocol.MakeErrReply("ERR hash value is not a float")
	}
	result := val.Add(delta)
	resultBytes := []byte(result.String())
	d.Put(field, resultBytes)
	db.addAof(utils.ToCmdLine3("hincrbyfloat", args...))
	return protocol.MakeBulkReply(resultBytes)
}

// execHRandField return a random field(or field-value) from the hash value stored at key.
func execHRandField(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	count := 1
	withvalues := 0

	if len(args) > 3 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'hrandfield' command")
	}

	if len(args) == 3 {
		if strings.ToLower(string(args[2])) == "withvalues" {
			withvalues = 1
		} else {
			return protocol.MakeSyntaxErrReply()
		}
	}

	if len(args) >= 2 {
		count64, err := strconv.ParseInt(string(args[1]), 10, 64)
		if err != nil {
			return protocol.MakeErrReply("ERR value is not an integer or out of range")
		}
		count = int(count64)
	}

	d, errReply := db.getAsDict(key)
	if errReply != nil {
		return errReply
	}
	if d == nil {
		return &protocol.EmptyMultiBulkReply{}
	}

	if count > 0 {
		fields := d.RandomDistinctKeys(count)
		Numfield := len(fields)
		if withvalues == 0 {
			result := make([][]byte, Numfield)
			for i, v := range fields {
				result[i] = []byte(v)
			}
			return protocol.MakeMultiBulkReply(result)
		} else {
			result := make([][]byte, 2*Numfield)
			for i, v := range fields {
				result[2*i] = []byte(v)
				raw, _ := d.Get(v)
				result[2*i+1] = raw.([]byte)
			}
			return protocol.MakeMultiBulkReply(result)
		}
	} else if count < 0 {
		fields := d.RandomKeys(-count)
		Numfield := len(fields)
		if withvalues == 0 {
			result := make([][]byte, Numfield)
			for i, v := range fields {
				result[i] = []byte(v)
			}
			return protocol.MakeMultiBulkReply(result)
		} else {
			result := make([][]byte, 2*Numfield)
			for i, v := range fields {
				result[2*i] = []byte(v)
				raw, _ := d.Get(v)
				result[2*i+1] = raw.([]byte)
			}
			return protocol.MakeMultiBulkReply(result)
		}
	}

	// 'count' is 0 will reach.
	return &protocol.EmptyMultiBulkReply{}
}

func init() {
	RegisterCommand("HSet", execHSet, writeFirstKey, undoHSet, 4, flagWrite)
	RegisterCommand("HSetNX", execHSetNX, writeFirstKey, undoHSet, 4, flagWrite)
	RegisterCommand("HGet", execHGet, readFirstKey, nil, 3, flagReadOnly)
	RegisterCommand("HExists", execHExists, readFirstKey, nil, 3, flagReadOnly)
	RegisterCommand("HDel", execHDel, writeFirstKey, undoHDel, -3, flagWrite)
	RegisterCommand("HLen", execHLen, readFirstKey, nil, 2, flagReadOnly)
	RegisterCommand("HStrlen", execHStrLen, readFirstKey, nil, 3, flagReadOnly)
	RegisterCommand("HMSet", execHMSet, writeFirstKey, undoHMSet, -4, flagWrite)
	RegisterCommand("HMGet", execHMGet, readFirstKey, nil, -3, flagReadOnly)
	RegisterCommand("HGet", execHGet, readFirstKey, nil, -3, flagReadOnly)
	RegisterCommand("HKeys", execHKeys, readFirstKey, nil, 2, flagReadOnly)
	RegisterCommand("HVals", execHVals, readFirstKey, nil, 2, flagReadOnly)
	RegisterCommand("HGetAll", execHGetAll, readFirstKey, nil, 2, flagReadOnly)
	RegisterCommand("HIncrBy", execHIncrBy, writeFirstKey, undoHIncr, 4, flagWrite)
	RegisterCommand("HIncrByFloat", execHIncrByFloat, writeFirstKey, undoHIncr, 4, flagWrite)
	RegisterCommand("HRandField", execHRandField, readFirstKey, nil, -2, flagReadOnly)
}
