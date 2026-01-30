import React, { Component } from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../utils/constants';

class DeleteSeatablesDialog extends Component {

  static propTypes = {
    t: PropTypes.func,
    accountName: PropTypes.string,
    onDeleteSeatables: PropTypes.func,
    closeDialog: PropTypes.func,
  };

  render () {
    const { accountName, closeDialog } = this.props;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Delete SeaTable base')}</h5>
              <button type="button" className="close" onClick={closeDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p className="pb-6">{gettext('Are you sure to delete SeaTable {accountName}?').replace('{accountName}', accountName)}</p>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={closeDialog}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.props.onDeleteSeatables}>{gettext('Delete')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

export default DeleteSeatablesDialog;
