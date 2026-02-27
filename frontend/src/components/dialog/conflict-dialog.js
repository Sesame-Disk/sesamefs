import React from 'react';
import PropTypes from 'prop-types';
import { gettext } from '../../utils/constants';

/**
 * Reusable conflict dialog for file revert operations.
 * Shown when reverting a file version returns 409 Conflict (file already exists
 * with different content). Offers three options: Replace, Keep Both, or Cancel.
 */
const ConflictDialog = ({ onReplace, onKeepBoth, onCancel }) => {
  return (
    <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)', position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, zIndex: 1050 }}>
      <div className="modal-dialog modal-dialog-centered">
        <div className="modal-content">
          <div className="modal-header">
            <h5 className="modal-title">{gettext('File Already Exists')}</h5>
            <button type="button" className="close" onClick={onCancel}>
              <span aria-hidden="true">&times;</span>
            </button>
          </div>
          <div className="modal-body">
            <p>{gettext('A file with this name already exists with different content.')}</p>
            <p>{gettext('What would you like to do?')}</p>
          </div>
          <div className="modal-footer">
            <button type="button" className="btn btn-secondary" onClick={onCancel}>
              {gettext('Cancel')}
            </button>
            <button type="button" className="btn btn-outline-primary" onClick={onKeepBoth}>
              {gettext('Keep Both')}
            </button>
            <button type="button" className="btn btn-primary" onClick={onReplace}>
              {gettext('Replace')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

ConflictDialog.propTypes = {
  onReplace: PropTypes.func.isRequired,
  onKeepBoth: PropTypes.func.isRequired,
  onCancel: PropTypes.func.isRequired,
};

export default ConflictDialog;
