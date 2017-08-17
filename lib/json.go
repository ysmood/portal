package lib

import (
	"fmt"
	"strconv"
)

func f2s(v interface{}) string {
	return strconv.FormatFloat(v.(float64), 'f', -1, 64)
}

func s2f(v interface{}) (f float64) {
	f, _ = strconv.ParseFloat(v.(string), 64)
	return
}

func isStr(v interface{}) (is bool) {
	_, is = v.(string)
	return
}

func isFloat64(v interface{}) (is bool) {
	_, is = v.(float64)
	return
}

func str(val interface{}) (str string) {
	switch val.(type) {
	case string:
		str = val.(string)
	case float64:
		str = f2s(val)
	default:
		str = fmt.Sprint(val)
	}
	return
}

func clone(obj interface{}) interface{} {
	switch obj.(type) {
	case map[string]interface{}:
		m := obj.(map[string]interface{})
		new := make(map[string]interface{}, len(m))
		for k, v := range m {
			new[k] = clone(v)
		}
		return new

	case []interface{}:
		arr := obj.([]interface{})
		new := make([]interface{}, len(arr))
		for k, v := range arr {
			new[k] = clone(v)
		}
		return new

	default:
		return obj
	}
}
