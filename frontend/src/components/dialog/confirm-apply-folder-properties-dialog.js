import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import toaster from '../toast';
import { Utils } from '../../utils/utils';
import Loading from '../loading';

import '../../css/apply-folder-properties.css';

const propTypes = {
  toggle: PropTypes.func,
  repoID: PropTypes.string,
  path: PropTypes.string
};

class ConfirmApplyFolderPropertiesDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      submitting: false
    };
  }

  submit = () => {
    const { repoID, path } = this.props;
    this.setState({ submitting: true });
    seafileAPI.applyFolderExtendedProperties(repoID, path).then(() => {
      toaster.success(gettext('Successfully applied the properties.'));
      this.props.toggle();
    }).catch(error => {
      let errorMsg = Utils.getErrorMsg(error);
      toaster.danger(errorMsg);
      this.setState({ submitting: false });
    });
  };

  render() {
    const { submitting } = this.state;

    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Apply properties')}</h5>
              <button type="button" className="close" onClick={this.props.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p>
            {gettext('Are you sure you want to apply the properties to all the files inside the folder?')}
          </p>
        </div>
        <div className="modal-footer">
          <Button color='secondary' onClick={this.props.toggle} disabled={submitting}>{gettext('Cancel')}</Button>
          <Button color='primary' className='flex-shrink-0 apply-properties' disabled={submitting} onClick={this.submit}>
            {submitting ? (<Loading />) : (<>{gettext('Submit')}</>)}
          </Button>
        </div>
      </div>
          </div>
        </div>
    );
  }

}

ConfirmApplyFolderPropertiesDialog.propTypes = propTypes;

export default ConfirmApplyFolderPropertiesDialog;
