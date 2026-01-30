import React, { Component } from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../utils/constants';

const propTypes = {
  removePassword: PropTypes.func.isRequired,
  toggle: PropTypes.func.isRequired
};

class RemoveWebdavPassword extends Component {

  constructor(props) {
    super(props);
    this.state = {
      btnDisabled: false
    };
  }

  submit = () => {
    this.setState({
      btnDisabled: true
    });

    this.props.removePassword();
  };

  render() {
    const { toggle } = this.props;

    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Delete WebDAV Password')}</h5>
              <button type="button" className="close" onClick={toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p>{gettext('Are you sure you want to delete WebDAV password?')}</p>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.submit} disabled={this.state.btnDisabled}>{gettext('Delete')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

RemoveWebdavPassword.propTypes = propTypes;

export default RemoveWebdavPassword;
