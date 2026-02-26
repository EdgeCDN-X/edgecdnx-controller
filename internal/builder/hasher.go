package builder

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
)

func HashValues[K any](values K) (string, error) {
	valuesBytes, err := json.Marshal(values)
	if err != nil {
		return "", err
	}

	hash := md5.Sum(valuesBytes)
	return fmt.Sprintf("%x", hash), nil
}
