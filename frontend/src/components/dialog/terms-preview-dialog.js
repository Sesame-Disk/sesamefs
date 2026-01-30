import React from 'react';
import PropTypes from 'prop-types';

import TermsPreviewWidget from '../terms-preview-widget';
import { gettext } from '../../utils/constants';

const propTypes = {
  title: PropTypes.string,
  content: PropTypes.string,
  onClosePreviewDialog: PropTypes.func.isRequired,
};

class TermsPreviewDialog extends React.Component {

  static defaultProps = {
    title: gettext('Terms'),
  };

  toggle = () => {
    this.props.onClosePreviewDialog();
  };

  render() {
    let { title, content } = this.props;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{title}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <TermsPreviewWidget content={content} />
        </div>
      </div>
          </div>
        </div>
    );
  }
}

TermsPreviewDialog.propTypes = propTypes;

export default TermsPreviewDialog;
