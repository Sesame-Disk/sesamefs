import React from 'react';
import PropTypes from 'prop-types';
import { Button, Form, FormGroup, Input } from 'reactstrap';
import { gettext } from '../../../utils/constants';

const propTypes = {
  value: PropTypes.string.isRequired,
  dialogTitle: PropTypes.string.isRequired,
  updateValue: PropTypes.func.isRequired,
  toggleDialog: PropTypes.func.isRequired
};

class UpdateUser extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      value: this.props.value,
      isSubmitBtnActive: false
    };
  }

  handleInputChange = (e) => {
    const value = e.target.value;
    this.setState({
      value: value
    });
  };

  handleKeyDown = (e) => {
    if (e.key == 'Enter') {
      this.handleSubmit();
      e.preventDefault();
    }
  };

  handleSubmit = () => {
    this.props.updateValue(this.state.value.trim());
    this.props.toggleDialog();
  };

  render() {
    const  { toggleDialog } = this.props;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{this.props.dialogTitle}</h5>
              <button type="button" className="close" onClick={toggleDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <Form>
            <FormGroup>
              <Input
                type="text"
                value={this.state.value}
                onKeyDown={this.handleKeyDown}
                onChange={this.handleInputChange}
              />
            </FormGroup>
          </Form>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={toggleDialog}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.handleSubmit}>{gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

UpdateUser.propTypes = propTypes;

export default UpdateUser;
