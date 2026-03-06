import React from 'react';
import PropTypes from 'prop-types';
import { Button, Form, FormGroup, Input } from 'reactstrap';
import { gettext } from '../../../utils/constants';

const propTypes = {
  name: PropTypes.string.isRequired,
  toggle: PropTypes.func.isRequired,
  updateName: PropTypes.func.isRequired
};

class SysAdminSetOrgNameDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      name: this.props.name,
      isSubmitBtnActive: false
    };
  }

  toggle = () => {
    this.props.toggle();
  };

  handleInputChange = (e) => {
    const value = e.target.value;
    this.setState({
      name: value,
      isSubmitBtnActive: value.trim() !== ''
    });
  };

  handleKeyDown = (e) => {
    if (e.key === 'Enter') {
      this.handleSubmit();
      e.preventDefault();
    }
  };

  handleSubmit = () => {
    this.props.updateName(this.state.name.trim());
    this.toggle();
  };

  render() {
    const { name, isSubmitBtnActive } = this.state;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
        <div className="modal-dialog modal-dialog-centered">
          <div className="modal-content">
            <div className="modal-header">
              <h5 className="modal-title">{gettext('Set Name')}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
            <div className="modal-body">
              <Form>
                <FormGroup>
                  <Input
                    type="text"
                    value={name}
                    onKeyDown={this.handleKeyDown}
                    onChange={this.handleInputChange}
                  />
                </FormGroup>
              </Form>
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

SysAdminSetOrgNameDialog.propTypes = propTypes;

export default SysAdminSetOrgNameDialog;
