import React, { Component } from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../utils/constants';

const propTypes = {
  restoreRepo: PropTypes.func.isRequired,
  toggle: PropTypes.func.isRequired
};

class ConfirmRestoreRepo extends Component {

  constructor(props) {
    super(props);
    this.state = {
      btnDisabled: false
    };
  }

  action = () => {
    this.setState({
      btnDisabled: true
    });
    this.props.restoreRepo();
  };

  render() {
    const { toggle } = this.props;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Restore Library')}</h5>
              <button type="button" className="close" onClick={toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p>{gettext('Are you sure you want to restore this library?')}</p>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.action} disabled={this.state.btnDisabled}>{gettext('Restore')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

ConfirmRestoreRepo.propTypes = propTypes;

export default ConfirmRestoreRepo;
