import React from 'react';
import PropTypes from 'prop-types';
import { Button, Form, FormGroup, Input } from 'reactstrap';
import { gettext } from '../../../utils/constants';

const propTypes = {
  toggle: PropTypes.func.isRequired,
  addInstitution: PropTypes.func.isRequired
};

class SysAdminAddInstitutionDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      value: '',
      isSubmitBtnActive: false
    };
  }

  handleChange = (e) => {
    const value = e.target.value;
    this.setState({
      value: value,
      isSubmitBtnActive: value.trim() != ''
    });
  };

  handleSubmit = () => {
    this.toggle();
    this.props.addInstitution(this.state.value.trim());
  };

  toggle = () => {
    this.props.toggle();
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Add institution')}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <Form>
            <p>{gettext('Name')}</p>
            <FormGroup>
              <Input
                value={this.state.value}
                onChange={this.handleChange}
              />
            </FormGroup>
          </Form>
        </div>
        <div className="modal-footer">
          <Button color="primary" onClick={this.handleSubmit} disabled={!this.state.isSubmitBtnActive}>{gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

SysAdminAddInstitutionDialog.propTypes = propTypes;

export default SysAdminAddInstitutionDialog;
