import React from 'react';
import PropTypes from 'prop-types';

import { gettext, mediaUrl, siteName } from '../../utils/constants';

const propTypes = {
  toggleDialog: PropTypes.func.isRequired
};

class GuideForNewDialog extends React.Component {

  toggle = () => {
    this.props.toggleDialog();
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-body">
          <button type="button" className="close text-gray" onClick={this.toggle}><span aria-hidden="true">×</span></button>
          <div className="p-2 text-center">
            <img src={`${mediaUrl}img/welcome.png`} width="408" alt="" />
            <h3 id="dialogTitle" className="mt-6 mb-4">{gettext('Welcome to {site_name_placeholder}').replace('{site_name_placeholder}', siteName)}</h3>
            {window.app.pageOptions.canAddRepo ?
              <p>{gettext('{site_name_placeholder} organizes files into libraries. Each library can be synced and shared separately. We have created a personal library for you. You can create more libraries later.').replace('{site_name_placeholder}', siteName)}</p> :
              <p>{gettext('{site_name_placeholder} organizes files into libraries. Each library can be synced and shared separately. However, since you are a guest user now, you can not create libraries.').replace('{site_name_placeholder}', siteName)}</p>
            }
          </div>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

GuideForNewDialog.propTypes = propTypes;

export default GuideForNewDialog;
