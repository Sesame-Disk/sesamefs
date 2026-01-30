import React from 'react';
import PropTypes from 'prop-types';
import { Button, Input, Form, FormGroup, Label } from 'reactstrap';
import { gettext } from '../../../utils/constants';
import { seafileAPI } from '../../../utils/seafile-api';
import { Utils } from '../../../utils/utils';
import toaster from '../../../components/toast';

const propTypes = {
  groupID: PropTypes.oneOfType([
    PropTypes.string,
    PropTypes.number
  ]).isRequired,
  name: PropTypes.string.isRequired,
  toggle: PropTypes.func.isRequired,
  onDepartmentNameChanged: PropTypes.func.isRequired
};

class RenameDepartmentDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      departmentName: this.props.name,
      errMessage: ''
    };
    this.newInput = React.createRef();
  }

  handleSubmit = () => {
    let isValid = this.validateName();
    if (isValid) {
      seafileAPI.sysAdminRenameDepartment(this.props.groupID, this.state.departmentName.trim()).then((res) => {
        this.props.toggle();
        this.props.onDepartmentNameChanged(res.data);
        toaster.success(gettext('Success'));
      }).catch(error => {
        let errorMsg = Utils.getErrorMsg(error);
        this.setState({ errMessage: errorMsg });
      });
    }
  };

  validateName = () => {
    let errMessage = '';
    const name = this.state.departmentName.trim();
    if (!name.length) {
      errMessage = gettext('Name is required');
      this.setState({ errMessage: errMessage });
      return false;
    }
    return true;
  };

  handleChange = (e) => {
    this.setState({
      departmentName: e.target.value
    });
  };

  handleKeyDown = (e) => {
    if (e.key === 'Enter') {
      this.handleSubmit();
      e.preventDefault();
    }
  };

  onAfterModelOpened = () => {
    if (!this.newInput.current) return;
    this.newInput.current.focus();
    this.newInput.current.select();
  };

  render() {
    let header = gettext('Rename Department');
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{header}</h5>
              <button type="button" className="close" onClick={this.props.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <Form>
            <FormGroup>
              <Label for="departmentName">{gettext('Name')}</Label>
              <Input
                id="departmentName"
                onKeyDown={this.handleKeyDown}
                value={this.state.departmentName}
                onChange={this.handleChange}
                innerRef={this.newInput}
              />
            </FormGroup>
          </Form>
          {this.state.errMessage && <p className="error">{this.state.errMessage}</p>}
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.props.toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.handleSubmit}>{gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

RenameDepartmentDialog.propTypes = propTypes;

export default RenameDepartmentDialog;
