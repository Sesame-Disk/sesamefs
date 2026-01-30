import React from 'react';
import PropTypes from 'prop-types';
import { gettext } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import { Input, Button } from 'reactstrap';
import toaster from '../toast';

class RenameGroupDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      newGroupName: this.props.currentGroupName,
      isSubmitBtnActive: false,
    };
  }

  handleGroupNameChange = (event) => {
    if (!event.target.value.trim()) {
      this.setState({isSubmitBtnActive: false});
    } else {
      this.setState({isSubmitBtnActive: true});
    }

    let name = event.target.value;
    this.setState({
      newGroupName: name
    });
  };

  renameGroup = () => {
    let name = this.state.newGroupName.trim();
    if (name) {
      let that = this;
      seafileAPI.renameGroup(this.props.groupID, name).then((res)=> {
        that.props.loadGroup(this.props.groupID);
        that.props.onGroupChanged(res.data.id);
      }).catch(error => {
        let errMessage = Utils.getErrorMsg(error);
        toaster.danger(errMessage);
      });
    }
    this.setState({
      newGroupName: '',
    });
    this.props.toggleRenameGroupDialog();
  };

  handleKeyDown = (event) => {
    if (event.keyCode === 13) {
      this.renameGroup();
    }
  };

  render() {
    return(
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Rename Group')}</h5>
              <button type="button" className="close" onClick={this.props.toggleRenameGroupDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <label htmlFor="newGroupName">{gettext('Rename group to')}</label>
          <Input type="text" id="newGroupName" value={this.state.newGroupName}
            onChange={this.handleGroupNameChange} onKeyDown={this.handleKeyDown}/>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.props.toggleRenameGroupDialog}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.renameGroup} disabled={!this.state.isSubmitBtnActive}>{gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

const RenameGroupDialogPropTypes = {
  showRenameGroupDialog: PropTypes.bool.isRequired,
  toggleRenameGroupDialog: PropTypes.func.isRequired,
  loadGroup: PropTypes.func.isRequired,
  groupID: PropTypes.string,
  onGroupChanged: PropTypes.func.isRequired,
  currentGroupName: PropTypes.string.isRequired,
};

RenameGroupDialog.propTypes = RenameGroupDialogPropTypes;

export default RenameGroupDialog;
