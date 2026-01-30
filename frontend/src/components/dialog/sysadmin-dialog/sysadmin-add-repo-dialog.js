import React from 'react';
import PropTypes from 'prop-types';
import { Button, Input, Form, FormGroup, Label } from 'reactstrap';
import { gettext } from '../../../utils/constants';
import { seafileAPI } from '../../../utils/seafile-api';
import { Utils } from '../../../utils/utils';

const propTypes = {
  toggle: PropTypes.func.isRequired,
  groupID: PropTypes.string,
  onAddNewRepo: PropTypes.func.isRequired
};

class AddRepoDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      repoName: '',
      errMessage: '',
    };
  }

  handleSubmit = () => {
    let isValid = this.validateName();
    if (isValid) {
      seafileAPI.sysAdminAddRepoInDepartment(this.props.groupID, this.state.repoName.trim()).then((res) => {
        this.props.toggle();
        this.props.onAddNewRepo(res.data);
      }).catch(error => {
        let errorMsg = Utils.getErrorMsg(error);
        this.setState({ errMessage: errorMsg });
      });
    }
  };

  validateName = () => {
    let errMessage = '';
    const name = this.state.repoName.trim();
    if (!name.length) {
      errMessage = gettext('Name is required');
      this.setState({ errMessage: errMessage });
      return false;
    }
    return true;
  };

  handleChange = (e) => {
    this.setState({
      repoName: e.target.value,
    });
  };

  handleKeyDown = (e) => {
    if (e.key === 'Enter') {
      this.handleSubmit();
      e.preventDefault();
    }
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('New Library')}</h5>
              <button type="button" className="close" onClick={this.props.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <Form>
            <FormGroup>
              <Label for="repoName">{gettext('Name')}</Label>
              <Input
                id="repoName"
                onKeyDown={this.handleKeyDown}
                value={this.state.repoName}
                onChange={this.handleChange}
                autoFocus={true}
              />
            </FormGroup>
          </Form>
          {this.state.errMessage && <p className="error">{this.state.errMessage}</p> }
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

AddRepoDialog.propTypes = propTypes;

export default AddRepoDialog;
