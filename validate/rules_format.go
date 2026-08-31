package validate

import (
	"regexp"

	"gochen/errors"
)

var (
	emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
)

// Email 校验邮箱地址格式。
func Email(email string) error {
	if email == "" {
		return errors.NewCode(errors.Validation, "email cannot be empty")
	}

	if !emailRegex.MatchString(email) {
		return errors.NewCode(errors.Validation, "invalid email format")
	}
	return nil
}

// Username 校验用户名是否满足长度与字符集约束。
func Username(username string) error {
	if err := Required(username, "username"); err != nil {
		return err
	}

	if err := StringLength(username, "username", 3, 50); err != nil {
		return err
	}

	if !usernameRegex.MatchString(username) {
		return errors.NewCode(errors.Validation,
			"username can only contain letters, numbers and underscores")
	}
	return nil
}

// Password 校验密码是否满足最基本的长度要求。
func Password(password string) error {
	if err := Required(password, "password"); err != nil {
		return err
	}

	if err := StringLength(password, "password", 6, 100); err != nil {
		return err
	}

	return nil
}

// PageParams 校验分页参数是否合法。
func PageParams(page, pageSize int) error {
	if page <= 0 {
		return errors.NewCode(errors.Validation, "page number must be greater than 0")
	}
	if pageSize <= 0 {
		return errors.NewCode(errors.Validation, "page size must be greater than 0")
	}
	if pageSize > 100 {
		return errors.NewCode(errors.Validation, "page size must not exceed 100")
	}
	return nil
}
