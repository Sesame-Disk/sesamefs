import React, { Component } from 'react';
import PropTypes from 'prop-types';
import { Alert, Button, Input, InputGroup, InputGroupAddon } from 'reactstrap';
import { gettext } from '../../utils/constants';
import { Utils } from '../../utils/utils';

const propTypes = {
  resetPassword: PropTypes.func.isRequired,
  toggle: PropTypes.func.isRequired
};

const { webdavSecretMinLength, webdavSecretStrengthLevel } = window.app.pageOptions;

class ResetWebdavPassword extends Component {

  constructor(props) {
    super(props);
    this.state = {
      password: '',
      isPasswordVisible: false,
      btnDisabled: false,
      errMsg: ''
    };
  }

  submit = () => {

    if (this.state.password.length === 0) {
      this.setState({errMsg: gettext('Please enter a password.')});
      return false;
    }
    if (this.state.password.length < webdavSecretMinLength) {
      this.setState({errMsg: gettext('The password is too short.')});
      return false;
    }

    if (Utils.getStrengthLevel(this.state.password) < webdavSecretStrengthLevel) {
      this.setState({errMsg: gettext('The password is too weak. It should include at least {passwordStrengthLevel} of the following: number, upper letter, lower letter and other symbols.').replace('{passwordStrengthLevel}', webdavSecretStrengthLevel)});
      return false;
    }

    this.setState({
      btnDisabled: true
    });

    this.props.resetPassword(this.state.password.trim());
  };

  handleInputChange = (e) => {
    this.setState({password: e.target.value});
  };

  togglePasswordVisible = () => {
    this.setState({
      isPasswordVisible: !this.state.isPasswordVisible
    });
  };

  generatePassword = () => {
    let randomPassword = Utils.generatePassword(webdavSecretMinLength);
    this.setState({
      password: randomPassword
    });
  };

  render() {
    const { toggle } = this.props;
    const passwordTip = gettext('(at least {passwordMinLength} characters and includes {passwordStrengthLevel} of the following: number, upper letter, lower letter and other symbols)').replace('{passwordMinLength}', webdavSecretMinLength).replace('{passwordStrengthLevel}', webdavSecretStrengthLevel);

    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Reset WebDAV Password')}</h5>
              <button type="button" className="close" onClick={toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <InputGroup>
            <Input type={this.state.isPasswordVisible ? 'text' : 'password'} value={this.state.password} onChange={this.handleInputChange} autoComplete="new-password"/>
            <InputGroupAddon addonType="append">
              <Button onClick={this.togglePasswordVisible}><i className={`fas ${this.state.isPasswordVisible ? 'fa-eye': 'fa-eye-slash'}`}></i></Button>
              <Button onClick={this.generatePassword}><i className="fas fa-magic"></i></Button>
            </InputGroupAddon>
          </InputGroup>
          <p className="form-text text-muted m-0">{passwordTip}</p>
          {this.state.errMsg && <Alert color="danger" className="m-0 mt-2">{gettext(this.state.errMsg)}</Alert>}
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.submit} disabled={this.state.btnDisabled}>{gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

ResetWebdavPassword.propTypes = propTypes;

export default ResetWebdavPassword;
