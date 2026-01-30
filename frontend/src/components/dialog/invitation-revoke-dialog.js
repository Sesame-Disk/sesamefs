import React from 'react';
import PropTypes from 'prop-types';
import { gettext } from '../../utils/constants';
import { Button } from 'reactstrap';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import Loading from '../loading';
import toaster from '../toast';

const propTypes = {
  accepter: PropTypes.string.isRequired,
  token: PropTypes.string.isRequired,
  revokeInvitation: PropTypes.func.isRequired,
  toggleDialog: PropTypes.func.isRequired
};

class InvitationRevokeDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      isSubmitting: false
    };
  }

  onRevokeInvitation = () => {
    this.setState({
      isSubmitting: true,
    });

    seafileAPI.revokeInvitation(this.props.token).then((res) => {
      this.props.revokeInvitation();
      this.props.toggleDialog();
      const msg = gettext('Successfully revoked access of user {placeholder}.').replace('{placeholder}', this.props.accepter);
      toaster.success(msg);
    }).catch((error) => {
      const errorMsg = Utils.getErrorMsg(error);
      toaster.danger(errorMsg);
      this.props.toggleDialog();
    });
  };

  render() {
    const { toggleDialog } = this.props;
    const { isSubmitting } = this.state;
    const email = '<span class="op-target">' + Utils.HTMLescape(this.props.accepter) + '</span>';
    const content = gettext('Are you sure to revoke access of user {placeholder} ?').replace('{placeholder}', email);

    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Revoke Access')}</h5>
              <button type="button" className="close" onClick={toggleDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p dangerouslySetInnerHTML={{__html: content}}></p>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={toggleDialog}>{gettext('Cancel')}</Button>
          <Button className="submit-btn" color="primary" onClick={this.onRevokeInvitation} disabled={isSubmitting}>{isSubmitting ? <Loading /> : gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

InvitationRevokeDialog.propTypes = propTypes;

export default InvitationRevokeDialog;
