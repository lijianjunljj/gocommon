package image

import "encoding/base64"

func Base64Decode(data string) (error, []byte) {
	dist, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return err, nil
	}
	return nil, dist
}

func Base64Encode(src []byte) (error, string) {
	dist := base64.StdEncoding.EncodeToString(src)
	return nil, dist
}
