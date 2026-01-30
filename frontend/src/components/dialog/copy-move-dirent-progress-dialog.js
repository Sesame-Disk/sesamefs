import React from 'react';
import PropTypes from 'prop-types';

import { gettext } from '../../utils/constants';

const propTypes = {
  type: PropTypes.oneOf(['move', 'copy']).isRequired,
  asyncOperatedFilesLength: PropTypes.number.isRequired,
  asyncOperationProgress: PropTypes.oneOfType([PropTypes.string, PropTypes.number]).isRequired,
  toggleDialog: PropTypes.func.isRequired,
};

class CopyMoveDirentProgressDialog extends React.Component {

  render() {

    let { type , asyncOperationProgress, asyncOperatedFilesLength } = this.props;
    let title = type === 'move' ? gettext('Move {num} items') : gettext('Copy {num} items');
    title = title.replace('{num}', asyncOperatedFilesLength);
    let progressStyle = {
      width: asyncOperationProgress + '%',
      lineHeight: '40px',
      textAlign: 'left',
    };
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{title}</h5>
              <button type="button" className="close" onClick={this.props.toggleDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body" style={{minHeight: '80px'}}>
          <div className="progress" style={{height: '40px'}}>
            <div
              className="progress-bar pl-2"
              role="progressbar"
              style={progressStyle}
              aria-valuenow={asyncOperationProgress}
              aria-valuemin="0"
              aria-valuemax="100"
            >
              {asyncOperationProgress + '%'}
            </div>
          </div>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

CopyMoveDirentProgressDialog.propTypes = propTypes;

export default CopyMoveDirentProgressDialog;
