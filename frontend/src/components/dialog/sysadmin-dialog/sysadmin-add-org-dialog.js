import React from 'react';
import PropTypes from 'prop-types';
import { Alert, Button, Form, FormGroup, Label, Input } from 'reactstrap';
import { gettext } from '../../../utils/constants';

const propTypes = {
  toggleDialog: PropTypes.func.isRequired,
  addOrg: PropTypes.func.isRequired
};

class SysAdminAddOrgDialog extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      name: '',
      email: '',
      password: '',
      passwordAgain: '',
      errorMsg: '',
      isSubmitBtnActive: false
    };
  }

  checkSubmitBtnActive = () => {
    const { name, email, password, passwordAgain } = this.state;
    let btnActive = true;
    if (name.trim() != '' &&
      email.trim() !== '' &&
      password.trim() !== '' &&
      passwordAgain.trim() !== '') {
      btnActive = true;
    } else {
      btnActive = false;
    }
    this.setState({
      isSubmitBtnActive: btnActive
    });
  };

  toggle = () => {
    this.props.toggleDialog();
  };

  inputPassword = (e) => {
    let passwd = e.target.value;
    this.setState({
      password: passwd
    }, this.checkSubmitBtnActive);
  };

  inputPasswordAgain = (e) => {
    let passwd = e.target.value;
    this.setState({
      passwordAgain: passwd
    }, this.checkSubmitBtnActive);
  };

  inputEmail = (e) => {
    let email = e.target.value;
    this.setState({
      email: email
    }, this.checkSubmitBtnActive);
  };

  inputName = (e) => {
    let name = e.target.value;
    this.setState({
      name: name
    }, this.checkSubmitBtnActive);
  };

  handleSubmit = () => {
    let { name, email, password, passwordAgain } = this.state;
    if (password !== passwordAgain) {
      this.setState({ errorMsg: gettext('Passwords do not match.') });
      return;
    }
    const data = {
      orgName: name.trim(),
      ownerEmail: email.trim(),
      password: password.trim()
    };
    this.props.addOrg(data);
    this.toggle();
  };

  render() {
    const { errorMsg, password, passwordAgain, email, name, isSubmitBtnActive } = this.state;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
        <div className="modal-dialog modal-dialog-centered">
          <div className="modal-content">
            <div className="modal-header">
              <h5 className="modal-title">{gettext('Add Organization')}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
            <div className="modal-body">
              <Form autoComplete="off">
                <FormGroup>
                  <Label>{gettext('Name')}</Label>
                  <Input value={name} onChange={this.inputName} />
                </FormGroup>
                <FormGroup>
                  <Label>
                    {gettext('Owner')}
                    <span className="small text-secondary ml-1 fas fa-question-circle" title={gettext('Owner can use admin panel in an organization, must be a new account.')}></span>
                  </Label>
                  <Input value={email} onChange={this.inputEmail} />
                </FormGroup>
                <FormGroup>
                  <Label>{gettext('Password')}</Label>
                  <Input type="password" value={password} onChange={this.inputPassword} />
                </FormGroup>
                <FormGroup>
                  <Label>{gettext('Password again')}</Label>
                  <Input type="password" value={passwordAgain} onChange={this.inputPasswordAgain} />
                </FormGroup>
              </Form>
              {errorMsg && <Alert color="danger">{errorMsg}</Alert>}
            </div>
            <div className="modal-footer">
              <Button color="secondary" onClick={this.toggle}>{gettext('Cancel')}</Button>
              <Button color="primary" onClick={this.handleSubmit} disabled={!isSubmitBtnActive}>{gettext('Submit')}</Button>
            </div>
          </div>
        </div>
      </div>
    );
  }
}

SysAdminAddOrgDialog.propTypes = propTypes;

export default SysAdminAddOrgDialog;
