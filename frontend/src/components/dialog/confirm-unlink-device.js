import React, { Component } from 'react';
import PropTypes from 'prop-types';
import { Button, FormGroup, Label, Input } from 'reactstrap';
import { gettext } from '../../utils/constants';

const propTypes = {
  executeOperation: PropTypes.func.isRequired,
  toggleDialog: PropTypes.func.isRequired
};

class ConfirmUnlinkDevice extends Component {

  constructor(props) {
    super(props);
    this.state = {
      isChecked: false
    };
  }

  toggle = () => {
    this.props.toggleDialog();
  };

  executeOperation = () => {
    this.toggle();
    this.props.executeOperation(this.state.isChecked);
  };

  onInputChange = (e) => {
    this.setState({
      isChecked: e.target.checked
    });
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Unlink device')}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p>{gettext('Are you sure you want to unlink this device?')}</p>
          <FormGroup check>
            <Label check>
              <Input type="checkbox" checked={this.state.isChecked} onChange={this.onInputChange} />
              <span>{gettext('Delete files from this device the next time it comes online.')}</span>
            </Label>
          </FormGroup>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.executeOperation}>{gettext('Unlink')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

ConfirmUnlinkDevice.propTypes = propTypes;

export default ConfirmUnlinkDevice;
